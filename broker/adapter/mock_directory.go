package adapter

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
)

var MOCK_PEER_URL = common.GetEnvWithDeprecated("MOCK_PEER_URL", "MOCK_CLIENT_URL", "http://localhost:19083/iso18626")

type MockDirectoryLookupAdapter struct {
}

func (m *MockDirectoryLookupAdapter) Lookup(ctx common.ExtendedContext, params DirectoryLookupParams) ([]DirectoryEntry, string, error) {
	if params.EntryID != "" {
		id, err := uuid.Parse(params.EntryID)
		if err != nil {
			return nil, "", fmt.Errorf("invalid directory entry ID: %w", err)
		}
		// Like symbol lookups, synthesize a usable entry for the requested identity.
		pickupCode := id.String()
		name := "Mock pickup location " + pickupCode
		return []DirectoryEntry{{
			Name:       name,
			URL:        MOCK_PEER_URL,
			Vendor:     dirapi.Unknown,
			BrokerMode: DEFAULT_BROKER_MODE,
			CustomData: dirapi.Entry{
				Id:        &id,
				Name:      name,
				LmsConfig: &dirapi.LmsConfig{RequesterPickupLocation: &pickupCode},
				Addresses: &[]dirapi.Address{{
					Type:              "Shipping",
					AddressComponents: &[]dirapi.AddressComponent{{Type: "Thoroughfare", Value: "1 Mock Library Street"}},
				}},
			},
		}}, "/by-id/" + pickupCode, nil
	}
	if params.Tenant != "" {
		if params.Tenant == "tenanterror" {
			return []DirectoryEntry{}, "", errors.New("there is an error")
		}
		if params.Tenant == "tenantnotfound" {
			return []DirectoryEntry{}, "", nil
		}
		if params.Tenant == "tenantmultiple" {
			return []DirectoryEntry{{
				Symbols:    []string{"ISIL:D1", "ISIL:D2"},
				URL:        MOCK_PEER_URL,
				Vendor:     dirapi.Unknown,
				BrokerMode: DEFAULT_BROKER_MODE,
			}, {
				Symbols:    []string{"ISIL:D3", "ISIL:D4"},
				URL:        MOCK_PEER_URL,
				Vendor:     dirapi.Unknown,
				BrokerMode: DEFAULT_BROKER_MODE,
			}}, "tenant lookup", nil
		}
		return []DirectoryEntry{{
			Symbols:    []string{"ISIL:" + strings.ToUpper(params.Tenant)},
			URL:        MOCK_PEER_URL,
			Vendor:     dirapi.Unknown,
			BrokerMode: DEFAULT_BROKER_MODE,
		}}, "tenant lookup", nil
	}
	if len(params.Symbols) == 0 {
		return []DirectoryEntry{}, "", errors.New("no symbols provided")
	}
	if strings.Contains(params.Symbols[0], "error") {
		return []DirectoryEntry{}, "", errors.New("there is an error")
	}
	if strings.Contains(params.Symbols[0], "d-not-found") {
		return []DirectoryEntry{}, strings.Join(params.Symbols, ","), nil
	}
	if strings.Contains(params.Symbols[0], "ISIL:NOCHANGE") {
		return []DirectoryEntry{{
			Symbols:    []string{"ISIL:NOCHANGE"},
			URL:        MOCK_PEER_URL,
			Vendor:     dirapi.Unknown,
			BrokerMode: DEFAULT_BROKER_MODE,
		}}, strings.Join(params.Symbols, ","), nil
	}

	var dirs []DirectoryEntry
	for _, value := range params.Symbols {
		dirs = append(dirs, DirectoryEntry{
			Symbols:    []string{value},
			URL:        MOCK_PEER_URL,
			Vendor:     dirapi.Unknown,
			BrokerMode: DEFAULT_BROKER_MODE,
		})
	}
	return dirs, strings.Join(params.Symbols, ","), nil
}

func (m *MockDirectoryLookupAdapter) FilterAndSort(ctx common.ExtendedContext, entries []Supplier, requesterData dirapi.Entry, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) ([]Supplier, RotaInfo) {
	var rotaInfo RotaInfo
	rotaInfo.Request.Type = "mock"
	rotaInfo.Suppliers = make([]SupplierMatch, 0, len(entries))
	filtered := make([]Supplier, 0, len(entries))
	for _, sup := range entries {
		applyHoldingsPolicy(&sup)
		match := holdingMatchesPolicy(sup)
		if match {
			filtered = append(filtered, sup)
		}
		rotaInfo.Suppliers = append(rotaInfo.Suppliers, SupplierMatch{
			Symbol:             sup.Symbol,
			Location:           sup.Location,
			ShelvingLocation:   sup.ShelvingLocation,
			ItemLoanPolicy:     sup.ItemLoanPolicy,
			LocationPreference: sup.LocationPreference,
			ShelvingPreference: sup.ShelvingPreference,
			Match:              match,
		})
	}

	slices.SortFunc(filtered, func(a, b Supplier) int {
		if a.Local && !b.Local {
			return -1
		} else if !a.Local && b.Local {
			return 1
		}
		return cmp.Compare(a.Ratio, b.Ratio)
	})
	return filtered, rotaInfo
}
