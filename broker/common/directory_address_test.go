package common

import (
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/require"
)

func TestDirectoryShippingAddressUsesFirstShippingAddressWithComponents(t *testing.T) {
	for _, tc := range []struct {
		name       string
		components *[]dirapi.AddressComponent
		want       iso18626.PhysicalAddress
	}{
		{
			name: "later addresses cannot overwrite or supplement fields",
			components: &[]dirapi.AddressComponent{
				{Type: "Thoroughfare", Value: "First Street"},
				{Type: "Locality", Value: "First City"},
				{Type: "AdministrativeArea", Value: "First Region"},
				{Type: "CountryCode", Value: "DK"},
			},
			want: iso18626.PhysicalAddress{
				Line1: "First Street", Locality: "First City",
				Region:  &iso18626.TypeSchemeValuePair{Text: "First Region"},
				Country: &iso18626.TypeSchemeValuePair{Text: "DK"},
			},
		},
		{
			name: "nil components skip to the next shipping address",
			want: iso18626.PhysicalAddress{Line1: "Second Street", PostalCode: "12345"},
		},
		{name: "empty components stop selection", components: &[]dirapi.AddressComponent{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := dirapi.Entry{Addresses: &[]dirapi.Address{
				{Type: "Billing", AddressComponents: &[]dirapi.AddressComponent{{Type: "Thoroughfare", Value: "Billing Street"}}},
				{Type: "Shipping", AddressComponents: tc.components},
				{Type: "Shipping", AddressComponents: &[]dirapi.AddressComponent{
					{Type: "Thoroughfare", Value: "Second Street"},
					{Type: "PostalCode", Value: "12345"},
				}},
			}}
			require.Equal(t, tc.want, DirectoryShippingAddress(entry))
		})
	}
}
