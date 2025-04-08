package adapter

import (
	"github.com/indexdata/crosslink/iso18626"
)

type DirectoryLookupAdapter interface {
	Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error)
	FilterAndSort(entries []SupplierToAdd, requesterData map[string]any, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) []SupplierToAdd
}

type DirectoryLookupParams struct {
	Symbols []string
}

type DirectoryEntry struct {
	Symbol     []string
	Name       string
	URL        string
	Vendor     string
	CustomData map[string]any
}

type SupplierToAdd struct {
	LocalIdentifier string
	PeerId          string
	CustomData      map[string]any
	Ratio           float32
	Symbol          string
	NetworkPriority int32
	Cost            float64
}
