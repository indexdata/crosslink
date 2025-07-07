package adapter

import (
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/iso18626"
)

var DEFAULT_BROKER_MODE extctx.BrokerMode

type DirectoryLookupAdapter interface {
	Lookup(params DirectoryLookupParams) ([]DirectoryEntry, error, string)
	FilterAndSort(ctx extctx.ExtendedContext, entries []Supplier, requesterData map[string]any, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) ([]Supplier, MatchResult)
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
	NetworkPriority int
	Cost            float64
	Local           bool
}

type MatchRequest struct {
	ServiceLevel string `json:"serviceLevel"`
	ServiceType  string `json:"serviceType"`
	Cost         string `json:"cost"`
}

type MatchRequester struct {
	Networks []string `json:"networks"`
}

type MatchValue struct {
	Value string `json:"value"`
	Match bool   `json:"match"`
}

type MatchTier struct {
	Tier         string `json:"tierId"`
	ServiceLevel string `json:"serviceLevel"`
	ServiceType  string `json:"serviceType"`
	Cost         string `json:"cost"`
	Match        bool   `json:"match"`
}

type MatchSupplier struct {
	Symbol   string       `json:"symbol"`
	Networks []MatchValue `json:"networks"`
	Match    bool         `json:"match"`
	Tiers    []MatchTier  `json:"tiers"`
}

type MatchResult struct {
	Request   MatchRequest    `json:"request"`
	Requester MatchRequester  `json:"requester"`
	Suppliers []MatchSupplier `json:"suppliers"`
}
