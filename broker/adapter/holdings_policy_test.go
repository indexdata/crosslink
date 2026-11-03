package adapter

import (
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
)

func TestRotaHoldingsPolicyDiagnostics(t *testing.T) {
	for _, directory := range []DirectoryLookupAdapter{new(ApiDirectory), new(MockDirectoryLookupAdapter)} {
		for _, tc := range []struct {
			name, status, reason string
			policy               *dirapi.HoldingsPolicy
			deferred             bool
		}{
			{"no policy", "not-configured", "", nil, false},
			{"deferred", "deferred", "", &dirapi.HoldingsPolicy{}, true},
			{"eligible", "evaluated", "", &dirapi.HoldingsPolicy{}, false},
			{"excluded", "evaluated", "location-excluded", &dirapi.HoldingsPolicy{Locations: &[]dirapi.HoldingsLocation{{Code: "MAIN", SupplyPreference: -1}}}, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				supplier := Supplier{HoldingItemID: "copy", Location: "MAIN", CustomData: dirapi.Entry{HoldingsPolicy: tc.policy}, DeferHoldingsPolicy: tc.deferred}
				_, rota := directory.FilterAndSort(common.CreateExtCtxWithArgs(t.Context(), nil), []Supplier{supplier}, dirapi.Entry{}, nil, nil, dirapi.LoadBalancingPolicyProportional)
				if !assert.Len(t, rota.Suppliers, 1) {
					return
				}
				candidate := rota.Suppliers[0]
				assert.Equal(t, "copy", candidate.ItemID)
				assert.Equal(t, tc.status, candidate.HoldingsPolicyStatus)
				assert.Equal(t, tc.reason, candidate.HoldingsPolicyReason)
				if tc.status == "evaluated" {
					if assert.NotNil(t, candidate.HoldingsPolicyEligible) {
						assert.Equal(t, tc.reason == "", *candidate.HoldingsPolicyEligible)
					}
				} else {
					assert.Nil(t, candidate.HoldingsPolicyEligible)
				}
			})
		}
	}
}

func TestHoldingsPolicyExclusionReasons(t *testing.T) {
	for _, tc := range []struct {
		name   string
		policy *dirapi.HoldingsPolicy
		want   string
	}{
		{"location", &dirapi.HoldingsPolicy{Locations: &[]dirapi.HoldingsLocation{{Code: "MAIN", SupplyPreference: -1}}}, "location-excluded"},
		{"shelving", &dirapi.HoldingsPolicy{ShelvingLocations: &[]dirapi.HoldingsShelvingLocation{{Code: "STACKS", SupplyPreference: -1}}}, "shelving-location-excluded"},
		{"loan policy", &dirapi.HoldingsPolicy{ItemLoanPolicies: &[]dirapi.HoldingsItemLoanPolicy{{Code: "restricted", Lendable: false}}}, "item-loan-policy-excluded"},
		{"no policy", nil, ""},
		{"unmatched rules", &dirapi.HoldingsPolicy{Locations: &[]dirapi.HoldingsLocation{{Code: "OTHER", SupplyPreference: -1}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			supplier := Supplier{Location: "MAIN", ShelvingLocation: "STACKS", ItemLoanPolicy: "restricted", HoldingItemID: "copy", CustomData: dirapi.Entry{HoldingsPolicy: tc.policy}}
			eligible, reason := EvaluateHoldingsPolicyWithReason(&supplier)
			assert.Equal(t, tc.want, reason)
			assert.Equal(t, tc.want == "", eligible)
			assert.Equal(t, eligible, EvaluateHoldingsPolicy(&supplier))
		})
	}
}

func TestApplyHoldingsPolicy(t *testing.T) {
	main := "MAIN"
	other := "OTHER"
	supplier := Supplier{
		Location:         "MAIN",
		ShelvingLocation: "STACKS",
		CustomData: dirapi.Entry{HoldingsPolicy: &dirapi.HoldingsPolicy{
			Locations: &[]dirapi.HoldingsLocation{
				{Code: "MAIN", SupplyPreference: 4},
			},
			ShelvingLocations: &[]dirapi.HoldingsShelvingLocation{
				{Code: "STACKS", SupplyPreference: 3},
			},
			LocationPolicies: &[]dirapi.HoldingsLocationPolicy{
				{ShelvingLocationCode: "STACKS", SupplyPreference: 5},
				{LocationCode: &other, ShelvingLocationCode: "STACKS", SupplyPreference: 6},
				{LocationCode: &main, ShelvingLocationCode: "STACKS", SupplyPreference: 7},
			},
		}},
	}

	applyHoldingsPolicy(&supplier)

	assert.Equal(t, 4, supplier.LocationPreference)
	assert.Equal(t, 7, supplier.ShelvingPreference, "exact override should beat the all-locations override")
}

func TestApplyHoldingsPolicyEmptyCollectionsAreNeutral(t *testing.T) {
	supplier := Supplier{
		Location:         "UNKNOWN",
		ShelvingLocation: "UNKNOWN",
		CustomData: dirapi.Entry{HoldingsPolicy: &dirapi.HoldingsPolicy{
			Locations:         &[]dirapi.HoldingsLocation{},
			ShelvingLocations: &[]dirapi.HoldingsShelvingLocation{},
			LocationPolicies:  &[]dirapi.HoldingsLocationPolicy{},
			ItemLoanPolicies:  &[]dirapi.HoldingsItemLoanPolicy{},
		}},
	}

	applyHoldingsPolicy(&supplier)

	assert.Zero(t, supplier.LocationPreference)
	assert.Zero(t, supplier.ShelvingPreference)
}

func TestApplyHoldingsPolicyOmittedCollectionsAreNeutral(t *testing.T) {
	supplier := Supplier{
		Location:         "UNKNOWN",
		ShelvingLocation: "UNKNOWN",
		CustomData:       dirapi.Entry{HoldingsPolicy: &dirapi.HoldingsPolicy{}},
	}

	applyHoldingsPolicy(&supplier)

	assert.Zero(t, supplier.LocationPreference)
	assert.Zero(t, supplier.ShelvingPreference)
}

func TestEvaluateHoldingsPolicyReplacesSelection(t *testing.T) {
	sup := Supplier{HoldingItemID: "item-1", ItemID: "stale", Location: "blocked",
		CustomData: dirapi.Entry{HoldingsPolicy: &dirapi.HoldingsPolicy{Locations: &[]dirapi.HoldingsLocation{{Code: "blocked", SupplyPreference: -1}}}},
	}
	assert.False(t, EvaluateHoldingsPolicy(&sup))
	assert.Empty(t, sup.ItemID)
	sup.Location = "allowed"
	assert.True(t, EvaluateHoldingsPolicy(&sup))
	assert.Equal(t, "item-1", sup.ItemID)
	assert.Zero(t, sup.LocationPreference)
	sup.HoldingItemID = "item-2"
	assert.True(t, EvaluateHoldingsPolicy(&sup))
	assert.Equal(t, "item-2", sup.ItemID)
	sup.HoldingItemID = ""
	assert.True(t, EvaluateHoldingsPolicy(&sup))
	assert.Empty(t, sup.ItemID)
	sup.HoldingItemID = "item-3"
	sup.DeferHoldingsPolicy = true
	sup.Location = "blocked"
	assert.True(t, EvaluateHoldingsPolicy(&sup))
	assert.Empty(t, sup.ItemID)
	sup.DeferHoldingsPolicy = false
	sup.CustomData.HoldingsPolicy = nil
	assert.True(t, EvaluateHoldingsPolicy(&sup))
	assert.Empty(t, sup.ItemID)
}
