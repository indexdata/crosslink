package service

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestCentralHoldingsLookupDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		status        events.EventStatus
		configure     func(*diagnosticLookup)
	}{
		{"holdings found", "available-holdings", events.EventStatusSuccess, func(*diagnosticLookup) {}},
		{"no record", "no-record", events.EventStatusProblem, func(l *diagnosticLookup) { l.Holdings = nil }},
		{"record with no available holdings", "no-available-holdings", events.EventStatusProblem, func(l *diagnosticLookup) { l.Holdings = nil; l.RecordFound = true }},
		{"lookup failure", "lookup-failed", events.EventStatusError, func(l *diagnosticLookup) { l.err = errors.New("lookup failed") }},
		{"parser failure", "holdings-failed", events.EventStatusError, func(l *diagnosticLookup) { l.HoldingsErr = errors.New("parser failed") }},
		{"nil result", "lookup-failed", events.EventStatusError, func(l *diagnosticLookup) { l.nilResult = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := metadataTestRepo(ill_db.IllTransaction{ID: "ill", RequesterID: pgtype.Text{String: "requester", Valid: true},
				IllTransactionData: ill_db.IllTransactionData{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "bib"}}},
				ill_db.Peer{ID: "requester"})
			lookup := &diagnosticLookup{query: "central-query", MockLookupAdapter: catalog.MockLookupAdapter{
				Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1", LocalIdentifier: "bib", ItemId: "copy"}},
			}}
			tc.configure(lookup)
			directory := new(adapter.MockDirectoryLookupAdapter)
			factory := NewLookupAdapterFactory(repo, directory, "", lookup, nil)
			locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, directory, factory)
			status, result := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill"})
			require.Equal(t, tc.status, status)
			details := result.CustomData["holdingsLookup"].(*availabilityLookupDetails)
			require.Equal(t, tc.outcome, details.Outcome)
			require.Equal(t, "bib", details.BibliographicID)
			require.Equal(t, "global", details.ConfigurationSource)
			require.Equal(t, tc.status == events.EventStatusSuccess, details.ResultApplied)
			if !lookup.nilResult {
				require.Equal(t, lookup.query, details.Query)
			}
			if status == events.EventStatusSuccess {
				require.Equal(t, 1, *details.AvailableHoldings)
				rota := result.CustomData[ROTA_INFO_KEY].(adapter.RotaInfo)
				require.Equal(t, "copy", rota.Suppliers[0].ItemID)
				require.Equal(t, "not-configured", rota.Suppliers[0].HoldingsPolicyStatus)
				require.Nil(t, rota.Suppliers[0].HoldingsPolicyEligible)
			}
		})
	}
}

func TestCentralLookupUsesConfiguredTargetOnlyWithoutGlobalOverride(t *testing.T) {
	for _, global := range []bool{false, true} {
		repo := metadataTestRepo(ill_db.IllTransaction{ID: "ill", RequesterID: pgtype.Text{String: "requester", Valid: true}},
			ill_db.Peer{ID: "requester", CustomData: dirapi.Entry{CatalogConfig: &dirapi.CatalogConfig{
				Sru: &dirapi.SruConfig{Address: "https://user:secret@catalog.example/sru?apiKey=secret#secret"},
			}}})
		lookup := &diagnosticLookup{MockLookupAdapter: catalog.MockLookupAdapter{Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1"}}}}
		directory := new(adapter.MockDirectoryLookupAdapter)
		factory := NewLookupAdapterFactory(repo, directory, "", nil, policyLookupCreator{lookup: lookup})
		if global {
			factory.globalLookupAdapter = lookup
		}
		locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, directory, factory)
		status, result := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill"})
		require.Equal(t, events.EventStatusSuccess, status)
		details := result.CustomData["holdingsLookup"].(*availabilityLookupDetails)
		if global {
			require.Equal(t, "global", details.ConfigurationSource)
			require.Empty(t, details.Target, "do not misreport an overridden Directory endpoint as the actual lookup target")
		} else {
			require.Equal(t, "directory", details.ConfigurationSource)
			require.Equal(t, "sru", details.Protocol)
			require.Equal(t, "https://catalog.example/sru", details.Target)
		}
	}
}

type failedDiagnosticDiscoveryRepo struct {
	*MockIllRepoLocateSuppliersWithSave
}

func (r *failedDiagnosticDiscoveryRepo) WithTxFunc(_ common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return fn(r)
}

func (r *failedDiagnosticDiscoveryRepo) SaveLocatedSupplier(common.ExtendedContext, ill_db.SaveLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("save failed")
}

func TestCentralHoldingsPolicyDiagnosticsSurviveSaveFailure(t *testing.T) {
	repo := &failedDiagnosticDiscoveryRepo{metadataTestRepo(ill_db.IllTransaction{ID: "ill", RequesterID: pgtype.Text{String: "requester", Valid: true}}, ill_db.Peer{ID: "requester"})}
	repo.peers[0].CustomData.HoldingsPolicy = &dirapi.HoldingsPolicy{}
	lookup := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{{Symbol: "ISIL:SUP1", ItemId: "copy"}}}
	directory := new(adapter.MockDirectoryLookupAdapter)
	factory := NewLookupAdapterFactory(repo, directory, "", lookup, nil)
	locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, directory, factory)
	status, result := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill"})
	require.Equal(t, events.EventStatusError, status)
	require.False(t, result.CustomData["holdingsLookup"].(*availabilityLookupDetails).ResultApplied)
	rota := result.CustomData[ROTA_INFO_KEY].(adapter.RotaInfo)
	require.Equal(t, "evaluated", rota.Suppliers[0].HoldingsPolicyStatus)
	require.True(t, *rota.Suppliers[0].HoldingsPolicyEligible)
}
