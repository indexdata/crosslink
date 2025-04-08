package adapter

import (
	"cmp"
	"errors"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"slices"
	"strings"
)

var MOCK_CLIENT_URL = utils.GetEnv("MOCK_CLIENT_URL", "http://localhost:19083/iso18626")

type MockDirectoryLookupAdapter struct {
}

func (m *MockDirectoryLookupAdapter) Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error) {
	if strings.Contains(params.Symbols[0], "error") {
		return []DirectoryEntry{}, errors.New("there is an error")
	}
	if strings.Contains(params.Symbols[0], "d-not-found") {
		return []DirectoryEntry{}, nil
	}
	if strings.Contains(params.Symbols[0], "ISIL:NOCHANGE") {
		return []DirectoryEntry{{
			Symbol: []string{"ISIL:NOCHANGE"},
			URL:    MOCK_CLIENT_URL,
			Vendor: "illmock",
		}}, nil
	}

	var dirs []DirectoryEntry
	for _, value := range params.Symbols {
		dirs = append(dirs, DirectoryEntry{
			Symbol: []string{value},
			URL:    MOCK_CLIENT_URL,
			Vendor: "illmock",
		})
	}
	return dirs, nil
}

func (m *MockDirectoryLookupAdapter) FilterAndSort(entries []SupplierToAdd, requesterData map[string]any, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) []SupplierToAdd {
	slices.SortFunc(entries, func(a, b SupplierToAdd) int {
		return cmp.Compare(a.Ratio, b.Ratio)
	})
	return entries
}
