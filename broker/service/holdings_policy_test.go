package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/mocks"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestSupplierAvailabilityConfigured(t *testing.T) {
	for _, tc := range []struct {
		name, config        string
		configured, invalid bool
	}{
		{"absent", `{}`, false, false},
		{"profile", `{"catalogConfig":{"profile":"Sierra"}}`, false, false},
		{"metadata", `{"catalogConfig":{"metadataUpdateMode":"none"}}`, false, false},
		{"parser", `{"catalogConfig":{"holdingsFormat":{"marc":{}}}}`, false, false},
		{"sru", `{"catalogConfig":{"sru":{"address":"https://catalog.example/sru"}}}`, true, false},
		{"zoom", `{"catalogConfig":{"zoom":{"address":"catalog.example:210/db"}}}`, true, false},
		{"empty endpoint", `{"catalogConfig":{"sru":{"address":""}}}`, false, true},
		{"invalid profile", `{"catalogConfig":{"profile":"Unknown"}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entry dirapi.Entry
			require.NoError(t, json.Unmarshal([]byte(tc.config), &entry))
			configured, err := supplierAvailabilityConfigured(entry)
			if tc.invalid {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.configured, configured)
		})
	}
}

type availabilityPolicyRepo struct {
	mocks.MockIllRepositorySuccess
	peer      ill_db.Peer
	supplier  ill_db.LocatedSupplier
	saveErr   error
	commitErr error
}

func (r *availabilityPolicyRepo) GetPeerById(common.ExtendedContext, string) (ill_db.Peer, error) {
	return r.peer, nil
}
func (r *availabilityPolicyRepo) GetLocatedSuppliersByIllTransactionAndStatus(common.ExtendedContext, ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{r.supplier}, nil
}
func (r *availabilityPolicyRepo) SaveLocatedSupplier(_ common.ExtendedContext, p ill_db.SaveLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	if r.saveErr != nil {
		return ill_db.LocatedSupplier{}, r.saveErr
	}
	r.supplier = ill_db.LocatedSupplier(p)
	return r.supplier, nil
}

type policyLookupCreator struct {
	lookup catalog.LookupAdapter
	err    error
}

func (c policyLookupCreator) GetAdapter(common.ExtendedContext, ill_db.Peer) (catalog.LookupAdapter, error) {
	return c.lookup, c.err
}

func TestAvailabilityPolicyReevaluation(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		holdings                                    []catalog.Holding
		lookupErr, holdingsErr, adapterErr, saveErr bool
		noPolicy                                    bool
		invalidConfig, unconfigured                 bool
		want                                        string
		skipped, unresolved                         bool
	}{
		{name: "select best eligible", holdings: []catalog.Holding{{ItemId: "blocked", Location: "blocked"}, {ItemId: "ordinary"}, {ItemId: "best", Location: "preferred"}}, want: "best"},
		{name: "eligible without item clears selection", holdings: []catalog.Holding{{Location: "preferred"}}},
		{name: "all excluded", holdings: []catalog.Holding{{ItemId: "blocked", Location: "blocked"}}, skipped: true},
		{name: "no holdings", skipped: true},
		{name: "no policy lookup failure retains fallback", noPolicy: true, lookupErr: true},
		{name: "lookup failure", lookupErr: true, unresolved: true},
		{name: "parser failure", holdingsErr: true, unresolved: true},
		{name: "adapter failure", adapterErr: true, unresolved: true},
		{name: "save failure", saveErr: true},
		{name: "invalid configuration clears selection", invalidConfig: true},
		{name: "central selection survives absent availability", unconfigured: true, want: "stale"},
		{name: "no policy does not select", noPolicy: true, holdings: []catalog.Holding{{ItemId: "raw"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entry dirapi.Entry
			require.NoError(t, json.Unmarshal([]byte(`{"catalogConfig":{"sru":{"address":"https://catalog.example/sru"}},"holdingsPolicy":{"locations":[{"code":"blocked","supplyPreference":-1},{"code":"preferred","supplyPreference":5}]}}`), &entry))
			repo := &availabilityPolicyRepo{peer: ill_db.Peer{CustomData: entry}, supplier: ill_db.LocatedSupplier{ID: "sup", SupplierID: "peer", IllTransactionID: "ill", SupplierStatus: ill_db.SupplierStateSelectedPg, LocalID: pgtype.Text{String: "bib", Valid: true}, ItemID: pgtype.Text{String: "stale", Valid: true}, HoldingsPolicyResolved: pgtype.Bool{Bool: true, Valid: true}}}
			if tc.noPolicy {
				repo.peer.CustomData.HoldingsPolicy = nil
			}
			lookup := &catalog.MockLookupAdapter{Holdings: tc.holdings}
			if tc.lookupErr {
				lookup.Err = errors.New("lookup failed")
			}
			if tc.holdingsErr {
				lookup.HoldingsErr = errors.New("parser failed")
			}
			creator := policyLookupCreator{lookup: lookup}
			if tc.invalidConfig {
				repo.peer.CustomData.CatalogConfig.Sru.Address = ""
			}
			if tc.unconfigured {
				repo.peer.CustomData.CatalogConfig = nil
				creator.lookup = nil
			}
			if tc.adapterErr {
				creator.err = errors.New("adapter failed")
			}
			if tc.saveErr {
				repo.saveErr = errors.New("save failed")
			}
			factory := NewLookupAdapterFactory(repo, new(adapter.MockDirectoryLookupAdapter), "", nil, creator)
			locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, new(adapter.MockDirectoryLookupAdapter), factory)
			status, result := locator.checkAvailability(appCtx, events.Event{IllTransactionID: "ill"})
			if tc.unresolved || tc.saveErr || tc.invalidConfig || tc.lookupErr {
				require.Equal(t, events.EventStatusError, status)
			} else {
				require.Equal(t, events.EventStatusSuccess, status)
			}
			if tc.saveErr {
				return
			}
			require.True(t, repo.supplier.HoldingsPolicyResolved.Valid)
			require.Equal(t, !tc.unresolved && !tc.invalidConfig && !tc.skipped, repo.supplier.HoldingsPolicyResolved.Bool)
			require.Equal(t, tc.want, repo.supplier.ItemID.String)
			require.Equal(t, tc.want != "", repo.supplier.ItemID.Valid)
			require.Equal(t, "bib", repo.supplier.LocalID.String)
			require.Equal(t, tc.skipped, repo.supplier.SupplierStatus == ill_db.SupplierStateSkippedPg)
			if !tc.noPolicy && !tc.invalidConfig && !tc.unconfigured {
				require.Equal(t, tc.unresolved, result.CustomData["holdingsPolicyUnresolved"])
			}
		})
	}
}

func TestLocateSuppliersPolicyStage(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "central", true: "availability"}[deferred], func(t *testing.T) {
			repo := metadataTestRepo(ill_db.IllTransaction{ID: "ill", RequesterID: pgtype.Text{String: "requester", Valid: true}}, ill_db.Peer{ID: "requester"})
			entry := dirapi.Entry{HoldingsPolicy: &dirapi.HoldingsPolicy{Locations: &[]dirapi.HoldingsLocation{{Code: "blocked", SupplyPreference: -1}}}}
			if deferred {
				entry.CatalogConfig = &dirapi.CatalogConfig{Sru: &dirapi.SruConfig{Address: "https://catalog.example/sru"}}
			}
			repo.peers[0].CustomData = entry
			lookup := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{
				{Symbol: "ISIL:SUP1", LocalIdentifier: "first-bib", ItemId: "blocked-item", Location: "blocked"},
				{Symbol: "ISIL:SUP1", LocalIdentifier: "second-bib", ItemId: "eligible-item"},
			}}
			directory := new(adapter.MockDirectoryLookupAdapter)
			factory := NewLookupAdapterFactory(repo, directory, "", lookup, nil)
			locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, directory, factory)
			status, _ := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill"})
			require.Equal(t, events.EventStatusSuccess, status)
			require.Len(t, repo.savedLocatedSuppliers, 1)
			selected := repo.savedLocatedSuppliers[0]
			if deferred {
				require.Equal(t, "first-bib", selected.LocalID.String)
				require.False(t, selected.ItemID.Valid)
			} else {
				require.Equal(t, "second-bib", selected.LocalID.String)
				require.Equal(t, "eligible-item", selected.ItemID.String)
			}
		})
	}
}

func (r *availabilityPolicyRepo) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	if err := fn(r); err != nil {
		return err
	}
	return r.commitErr
}
func (r *availabilityPolicyRepo) GetLocatedSupplierByIdForUpdate(common.ExtendedContext, string) (ill_db.LocatedSupplier, error) {
	return r.supplier, nil
}

func TestAvailabilityCommitFailure(t *testing.T) {
	repo := &availabilityPolicyRepo{supplier: ill_db.LocatedSupplier{ID: "sup", IllTransactionID: "ill", SupplierStatus: ill_db.SupplierStateSelectedPg}, commitErr: errors.New("commit failed")}
	factory := NewLookupAdapterFactory(repo, nil, "", nil, policyLookupCreator{})
	locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, nil, factory)
	status, result := locator.checkAvailability(appCtx, events.Event{IllTransactionID: "ill"})
	require.Equal(t, events.EventStatusError, status)
	require.Contains(t, result.EventError.Cause, "commit failed")
	require.NotContains(t, result.CustomData, AvailabilityKey)
}

func TestDeferredPolicySeesAllOpacCirculations(t *testing.T) {
	// Both copies belong to one holding. Truncating its circulations before policy
	// evaluation hides the eligible/preferred second copy.
	const response = `<searchRetrieveResponse xmlns="http://docs.oasis-open.org/ns/search-ws/sruResponse">
 <numberOfRecords>1</numberOfRecords><records><record><recordData>
 <opacRecord><bibliographicRecord><record xmlns="http://www.loc.gov/MARC21/slim"/></bibliographicRecord>
 <holdings><holding><localLocation>MAIN</localLocation><circulations>
 <circulation><availableNow value="1"/><itemId>first</itemId><temporaryLocation>FIRST</temporaryLocation></circulation>
 <circulation><availableNow value="1"/><itemId>second</itemId><temporaryLocation>SECOND</temporaryLocation></circulation>
 </circulations></holding></holdings></opacRecord>
 </recordData></record></records></searchRetrieveResponse>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(response))
	}))
	defer server.Close()
	for _, all := range []*bool{nil, new(false), new(true)} {
		for _, tc := range []struct {
			name          string
			first, second int
			noPolicy      bool
			want          string
			skipped       bool
		}{
			{name: "later eligible", first: -1, want: "second"},
			{name: "later preferred", second: 5, want: "second"},
			{name: "all rejected", first: -1, second: -1, skipped: true},
			{name: "no policy", noPolicy: true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				entry := dirapi.Entry{CatalogConfig: &dirapi.CatalogConfig{
					Sru:            &dirapi.SruConfig{Address: server.URL},
					HoldingsFormat: &dirapi.HoldingsParserConfig{Opac: &dirapi.OpacHoldingsParserConfig{AllCirculations: all, IncludeTemporaryLocation: new(true)}},
				}, HoldingsPolicy: &dirapi.HoldingsPolicy{ShelvingLocations: &[]dirapi.HoldingsShelvingLocation{
					{Code: "FIRST", SupplyPreference: tc.first}, {Code: "SECOND", SupplyPreference: tc.second},
				}}}
				if tc.noPolicy {
					entry.HoldingsPolicy = nil
				}
				repo := &availabilityPolicyRepo{peer: ill_db.Peer{CustomData: entry}, supplier: ill_db.LocatedSupplier{ID: "sup", SupplierID: "peer", IllTransactionID: "ill", SupplierStatus: ill_db.SupplierStateSelectedPg, LocalID: pgtype.Text{String: "bib", Valid: true}}}
				creator := catalog.NewLookupAdapterCreator(catalog.LookupAdapterZoom, "")
				factory := NewLookupAdapterFactory(repo, nil, "", nil, creator)
				locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, nil, factory)
				status, result := locator.checkAvailability(appCtx, events.Event{IllTransactionID: "ill"})
				require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
				require.Equal(t, tc.want, repo.supplier.ItemID.String)
				require.Equal(t, tc.skipped, repo.supplier.SupplierStatus == ill_db.SupplierStateSkippedPg)
				require.Equal(t, !tc.skipped, repo.supplier.HoldingsPolicyResolved.Bool)
				// Ordinary lookups also return all candidates, regardless of the legacy setting.
				lookup, err := creator.GetAdapter(appCtx, repo.peer)
				require.NoError(t, err)
				ordinary, err := lookup.Lookup(catalog.LookupParams{Identifier: "bib"})
				require.NoError(t, err)
				holdings, err := ordinary.GetHoldings()
				require.NoError(t, err)
				require.Len(t, holdings, 2)
			})
		}
	}
}
