package adapter

import (
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
)

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
