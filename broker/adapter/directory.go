package adapter

import (
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/iso18626"
)

var DEFAULT_BROKER_MODE extctx.BrokerMode

type DirectoryLookupAdapter interface {
	Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error, string)
	FilterAndSort(ctx extctx.ExtendedContext, entries []Supplier, requesterData map[string]any, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) []Supplier
}

type DirectoryLookupParams struct {
	Symbols []string
}

type DirectoryEntry struct {
	Symbols       []string
	BranchSymbols []string
	Name          string
	URL           string
	Vendor        extctx.Vendor
	BrokerMode    extctx.BrokerMode
	CustomData    map[string]any
}

type Supplier struct {
	LocalIdentifier string
	PeerId          string
	CustomData      map[string]any
	Ratio           float32
	Symbol          string
	NetworkPriority float64
	Cost            float64
	Local           bool
}
