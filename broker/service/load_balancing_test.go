package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProportionalLoadBalancingScore(t *testing.T) {
	for _, tc := range []struct {
		name           string
		borrows, loans int32
		ratio          *string
		want           float64
	}{
		{"lender A", 10, 1, nil, 0.1}, {"lender B", 100, 50, nil, 0.5},
		{"zero borrows", 0, 4, nil, 4}, {"zero counters", 0, 0, nil, 0},
		{"desired lending ratio", 10, 5, stringPointer("2:1"), 0.25},
		{"desired borrowing ratio", 10, 5, stringPointer("1:2"), 1},
		{"decimal ratio", 10, 5, stringPointer("0.5:2"), 2},
		{"largest score", 0, math.MaxInt32, stringPointer("0.01:9999.99"), float64(math.MaxInt32) * 999999},
	} {
		t.Run(tc.name, func(t *testing.T) {
			score, err := getLoadBalancingScore(ill_db.Peer{BorrowsCount: tc.borrows, LoansCount: tc.loans, CustomData: dirapi.Entry{LendToBorrowRatio: tc.ratio}}, dirapi.LoadBalancingPolicyProportional)
			require.NoError(t, err)
			assert.InDelta(t, tc.want, score, 1e-12)
			_, err = json.Marshal(adapter.RotaInfo{Suppliers: []adapter.SupplierMatch{{LoadBalancingScore: score}}})
			require.NoError(t, err)
		})
	}
	_, err := getLoadBalancingScore(ill_db.Peer{}, dirapi.LoadBalancingPolicy("invalid"))
	require.Error(t, err)
	_, err = getLoadBalancingScore(ill_db.Peer{CustomData: dirapi.Entry{LendToBorrowRatio: stringPointer("0:1")}}, dirapi.LoadBalancingPolicyProportional)
	require.Error(t, err)
}

func TestLocateSuppliersLoadBalancingPolicy(t *testing.T) {
	for _, tc := range []struct{ name, symbol, consortiumPolicy, requesterPolicy, first string }{
		{"default consortium policy ignores requester", "ISIL:SUPC", "", "deficit", "ISIL:SUP1"},
		{"deficit", "ISIL:SUPC", "deficit", "proportional", "ISIL:SUP2"},
		{"proportional", "ISIL:SUPC", "proportional", "deficit", "ISIL:SUP1"},
		{"no consortium ignores requester", "", "deficit", "deficit", "ISIL:SUP1"},
		{"invalid consortium policy", "ISIL:SUPC", "invalid", "deficit", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := func(policy string) *dirapi.IllConfig {
				cfg := &dirapi.IllConfig{}
				if policy != "" {
					cfg.LoadBalancingPolicy.Set(dirapi.LoadBalancingPolicy(policy))
				}
				return cfg
			}
			repo := &MockIllRepoLocateSuppliers{
				illTransaction:  ill_db.IllTransaction{ID: "ill-1", RequesterID: pgtype.Text{String: "requester", Valid: true}, IllTransactionData: ill_db.IllTransactionData{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "return-ISIL:SUP1::A;return-ISIL:SUP2::B"}}},
				requester:       ill_db.Peer{ID: "requester", CustomData: dirapi.Entry{IllConfig: config(tc.requesterPolicy)}},
				peers:           []ill_db.Peer{{ID: "A", BorrowsCount: 10, LoansCount: 1}, {ID: "B", BorrowsCount: 100, LoansCount: 50}},
				peerSymbols:     map[string][]ill_db.Symbol{"A": {{SymbolValue: "ISIL:SUP1", PeerID: "A"}}, "B": {{SymbolValue: "ISIL:SUP2", PeerID: "B"}}},
				consortiumPeers: []ill_db.Peer{{ID: "consortium", CustomData: dirapi.Entry{Symbols: &[]dirapi.Symbol{{Authority: "ISIL", Symbol: "SUPC"}}, IllConfig: config(tc.consortiumPolicy)}}},
			}
			ad := new(adapter.MockDirectoryLookupAdapter)
			factory := NewLookupAdapterFactory(repo, ad, tc.symbol, new(catalog.MockLookupShared), new(catalog.LookupAdapterCreatorImpl))
			locator := CreateSupplierLocator(new(events.PostgresEventBus), repo, ad, factory)
			status, result := locator.locateSuppliers(appCtx, events.Event{IllTransactionID: "ill-1"})
			if tc.first == "" {
				assert.Equal(t, events.EventStatusError, status)
				assert.Empty(t, repo.savedLocatedSuppliers)
				return
			}
			require.Equal(t, events.EventStatusSuccess, status, result)
			require.Len(t, repo.savedLocatedSuppliers, 2)
			assert.Equal(t, tc.first, repo.savedLocatedSuppliers[0].SupplierSymbol)
			rota := result.CustomData[ROTA_INFO_KEY].(adapter.RotaInfo)
			wantPolicy := dirapi.LoadBalancingPolicyProportional
			if tc.symbol != "" && tc.consortiumPolicy != "" {
				wantPolicy = dirapi.LoadBalancingPolicy(tc.consortiumPolicy)
			}
			assert.Equal(t, wantPolicy, rota.LoadBalancingPolicy)
			require.Len(t, rota.Suppliers, 2)
			assert.Equal(t, tc.first, rota.Suppliers[0].Symbol)
		})
	}
}

func TestLoadBalancingPolicyFallback(t *testing.T) {
	for _, tc := range []struct{ name, entry string }{
		{"missing illConfig", `{}`},
		{"null illConfig", `{"illConfig":null}`},
		{"omitted policy", `{"illConfig":{}}`},
		{"null policy", `{"illConfig":{"loadBalancingPolicy":null}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var entry dirapi.Entry
			require.NoError(t, json.Unmarshal([]byte(tc.entry), &entry))
			policy, err := (&LookupAdapterFactory{consortiumSymbol: "ISIL:C"}).loadBalancingPolicy(entry)
			require.NoError(t, err)
			assert.Equal(t, dirapi.LoadBalancingPolicyProportional, policy)
		})
	}
}
