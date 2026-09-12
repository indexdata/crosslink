package adapter

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockFilterAndSortAppliesHoldingsPolicy(t *testing.T) {
	main := "MAIN"
	data := dirapi.Entry{HoldingsPolicy: &dirapi.HoldingsPolicy{
		Locations: &[]dirapi.HoldingsLocation{
			{Code: "CLOSED", SupplyPreference: -1},
			{Code: "MAIN", SupplyPreference: 2},
		},
		ShelvingLocations: &[]dirapi.HoldingsShelvingLocation{
			{Code: "RESTRICTED", SupplyPreference: -1},
		},
		LocationPolicies: &[]dirapi.HoldingsLocationPolicy{
			{LocationCode: &main, ShelvingLocationCode: "RESTRICTED", SupplyPreference: 3},
		},
		ItemLoanPolicies: &[]dirapi.HoldingsItemLoanPolicy{
			{Code: "NONCIRC", Lendable: false},
			{Code: "NORMAL", Lendable: true},
		},
	}}
	for _, tc := range []struct {
		name     string
		supplier Supplier
		match    bool
		location int
		shelving int
	}{
		{name: "negative location", supplier: Supplier{CustomData: data, Location: "CLOSED"}, location: -1},
		{name: "negative shelf", supplier: Supplier{CustomData: data, ShelvingLocation: "RESTRICTED"}, shelving: -1},
		{name: "non-lendable item", supplier: Supplier{CustomData: data, ItemLoanPolicy: "NONCIRC"}},
		{name: "lendable item", supplier: Supplier{CustomData: data, ItemLoanPolicy: "NORMAL"}, match: true},
		{name: "exact override", supplier: Supplier{CustomData: data, Location: "MAIN", ShelvingLocation: "RESTRICTED"}, match: true, location: 2, shelving: 3},
		{name: "unknown codes", supplier: Supplier{CustomData: data, Location: "UNKNOWN", ShelvingLocation: "UNKNOWN", ItemLoanPolicy: "UNKNOWN"}, match: true},
		{name: "no policy", supplier: Supplier{}, match: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.supplier.Symbol = "A"
			ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
			filtered, rota := (&MockDirectoryLookupAdapter{}).FilterAndSort(ctx, []Supplier{tc.supplier}, dirapi.Entry{}, nil, nil)
			require.Len(t, rota.Suppliers, 1)
			assert.Equal(t, tc.match, rota.Suppliers[0].Match)
			assert.Equal(t, tc.location, rota.Suppliers[0].LocationPreference)
			assert.Equal(t, tc.shelving, rota.Suppliers[0].ShelvingPreference)
			assert.Equal(t, tc.supplier.Location, rota.Suppliers[0].Location)
			assert.Equal(t, tc.supplier.ShelvingLocation, rota.Suppliers[0].ShelvingLocation)
			assert.Equal(t, tc.supplier.ItemLoanPolicy, rota.Suppliers[0].ItemLoanPolicy)
			if tc.match {
				require.Len(t, filtered, 1)
				assert.Equal(t, tc.location, filtered[0].LocationPreference)
				assert.Equal(t, tc.shelving, filtered[0].ShelvingPreference)
			} else {
				assert.Empty(t, filtered)
			}
		})
	}
}

func TestMockDirectoryLookupByEntryID(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	id := uuid.New()
	adapter := &MockDirectoryLookupAdapter{}
	for _, params := range []DirectoryLookupParams{
		{EntryID: id.String()},
		{EntryID: id.String(), Symbols: []string{"error"}, Tenant: "tenanterror"},
	} {
		entries, query, err := adapter.Lookup(ctx, params)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Equal(t, "/by-id/"+id.String(), query)
		entry := entries[0]
		require.Equal(t, id, *entry.CustomData.Id)
		require.Equal(t, MOCK_PEER_URL, entry.URL)
		require.NotNil(t, entry.CustomData.LmsConfig)
		require.Equal(t, id.String(), *entry.CustomData.LmsConfig.RequesterPickupLocation)
		require.NotEmpty(t, common.DirectoryShippingAddress(entry.CustomData).Line1)
	}
	_, _, err := adapter.Lookup(ctx, DirectoryLookupParams{EntryID: "invalid"})
	require.ErrorContains(t, err, "invalid directory entry ID")
}
