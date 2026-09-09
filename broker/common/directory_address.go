package common

import (
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
)

// DirectoryShippingAddress converts the directory shipping address to ISO18626.
func DirectoryShippingAddress(entry dirapi.Entry) iso18626.PhysicalAddress {
	address := iso18626.PhysicalAddress{}
	if entry.Addresses != nil {
		for _, addr := range *entry.Addresses {
			if addr.AddressComponents != nil && addr.Type == "Shipping" {
				for _, comp := range *addr.AddressComponents {
					switch comp.Type {
					case "Thoroughfare":
						address.Line1 = comp.Value
					case "Locality":
						address.Locality = comp.Value
					case "AdministrativeArea":
						address.Region = &iso18626.TypeSchemeValuePair{
							Text: comp.Value,
						}
					case "PostalCode":
						address.PostalCode = comp.Value
					case "CountryCode":
						address.Country = &iso18626.TypeSchemeValuePair{
							Text: comp.Value,
						}
					}
				}
			}
		}
	}
	return address
}
