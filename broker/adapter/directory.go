package adapter

import (
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
)

var DEFAULT_BROKER_MODE common.BrokerMode

type DirectoryLookupAdapter interface {
	Lookup(params DirectoryLookupParams) ([]DirectoryEntry, string, error)
	FilterAndSort(ctx common.ExtendedContext, entries []Supplier, requesterData directory.Entry, serviceInfo *iso18626.ServiceInfo, billingInfo *iso18626.BillingInfo) ([]Supplier, RotaInfo)
}

type DirectoryLookupParams struct {
	Symbols []string
}

type DirectoryEntry struct {
	Symbols       []string
	BranchSymbols []string
	Name          string
	URL           string
	Vendor        directory.EntryVendor
	BrokerMode    common.BrokerMode
	CustomData    directory.Entry
}

type SupplierOrdering interface {
	GetSymbol() string
	GetPriority() int
	GetCost() float64
	GetRatio() float32
	IsLocal() bool
}

type Supplier struct {
	LocalIdentifier string
	PeerId          string
	CustomData      directory.Entry
	Symbol          string
	Priority        int
	Cost            float64
	Local           bool
	Ratio           float32
	SupplierStatus  pgtype.Text
}

func (s Supplier) GetSymbol() string { return s.Symbol }
func (s Supplier) GetPriority() int  { return s.Priority }
func (s Supplier) GetCost() float64  { return s.Cost }
func (s Supplier) IsLocal() bool     { return s.Local }
func (s Supplier) GetRatio() float32 { return s.Ratio }

type Network struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

type Tier struct {
	Name  string  `json:"name"`
	Cost  float64 `json:"cost"`
	Level string  `json:"level"`
	Type  string  `json:"type"`
}

type Request struct {
	Level string `json:"level"`
	Type  string `json:"type"`
	Cost  string `json:"cost"`
}

type Requester struct {
	Networks []string `json:"networks"`
}

type NetworkMatch struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Match    bool   `json:"match"`
}

type TierMatch struct {
	Name  string `json:"name"`
	Level string `json:"level"`
	Type  string `json:"type"`
	Cost  string `json:"cost"`
	Match bool   `json:"match"`
}

type SupplierMatch struct {
	Match    bool           `json:"match"`
	Networks []NetworkMatch `json:"networks"`
	Tiers    []TierMatch    `json:"tiers"`
	Symbol   string         `json:"symbol"`
	Priority int            `json:"priority"`
	Cost     string         `json:"cost"`
	Local    bool           `json:"local"`
	Ratio    float32        `json:"ratio"`
}

func (s SupplierMatch) GetSymbol() string { return s.Symbol }
func (s SupplierMatch) GetPriority() int  { return s.Priority }
func (s SupplierMatch) GetCost() float64 {
	f, _ := strconv.ParseFloat(s.Cost, 64)
	return f
}
func (s SupplierMatch) IsLocal() bool     { return s.Local }
func (s SupplierMatch) GetRatio() float32 { return s.Ratio }

type RotaInfo struct {
	Request   Request         `json:"request"`
	Requester Requester       `json:"requester"`
	Suppliers []SupplierMatch `json:"suppliers"`
}
