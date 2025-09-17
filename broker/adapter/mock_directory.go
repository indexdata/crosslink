package adapter

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

var MOCK_CLIENT_URL = utils.GetEnv("MOCK_CLIENT_URL", "http://localhost:19083/iso18626")

type MockDirectoryLookupAdapter struct {
}

func (m *MockDirectoryLookupAdapter) Lookup(params DirectoryLookupParams) ([]DirectoryEntry, string, error) {
	if strings.Contains(params.Symbols[0], "error") {
		return []DirectoryEntry{}, "", errors.New("there is an error")
	}
	if strings.Contains(params.Symbols[0], "d-not-found") {
		return []DirectoryEntry{}, strings.Join(params.Symbols, ","), nil
	}
	if strings.Contains(params.Symbols[0], "ISIL:NOCHANGE") {
		return []DirectoryEntry{{
			Symbols:    []string{"ISIL:NOCHANGE"},
			URL:        MOCK_CLIENT_URL,
			Vendor:     extctx.VendorUnknown,
			BrokerMode: DEFAULT_BROKER_MODE,
		}}, strings.Join(params.Symbols, ","), nil
	}

	var dirs []DirectoryEntry
	for _, value := range params.Symbols {
		dirs = append(dirs, DirectoryEntry{
			Symbols:    []string{value},
			URL:        MOCK_CLIENT_URL,
			Vendor:     extctx.VendorUnknown,
			BrokerMode: DEFAULT_BROKER_MODE,
		})
	}
	return dirs, strings.Join(params.Symbols, ","), nil
}

func (m *MockDirectoryLookupAdapter) FilterAndSort(ctx extctx.ExtendedContext, entries []Supplier, requesterData map[string]any, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) ([]Supplier, RotaInfo) {
	var rotaInfo RotaInfo
	rotaInfo.Request.Type = "mock"
	rotaInfo.Suppliers = make([]SupplierMatch, 0, len(entries))
	for _, sup := range entries {
		rotaInfo.Suppliers = append(rotaInfo.Suppliers, SupplierMatch{
			Symbol: sup.Symbol,
			Match:  true,
		})
	}

	slices.SortFunc(entries, func(a, b Supplier) int {
		if a.Local && !b.Local {
			return -1
		} else if !a.Local && b.Local {
			return 1
		}
		return cmp.Compare(a.Ratio, b.Ratio)
	})
	return entries, rotaInfo
}
