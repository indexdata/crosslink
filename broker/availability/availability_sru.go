package availability

import (
	"net/http"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
)

type SruAvailabilityAdapter struct {
	holdingsLookupAdapter adapter.HoldingsLookupAdapter
}

func NewSruAvailabilityAdapter(ctx common.ExtendedContext, config directory.AvailabilityConfig) (adapter.HoldingsLookupAdapter, error) {
	holdingsParser := adapter.NewMarcHoldingsParser(nil)
	queryBuilder := adapter.NewQueryBuilderCql(config.PqfMappings)
	a := &SruAvailabilityAdapter{
		holdingsLookupAdapter: adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{config.Address}, "", queryBuilder, holdingsParser),
	}
	return a, nil
}

func (a *SruAvailabilityAdapter) Lookup(params adapter.HoldingLookupParams) ([]adapter.Holding, string, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
