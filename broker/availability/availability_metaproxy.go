package availability

import (
	"net/http"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
)

type MetaproxyAvailabilityAdapter struct {
	holdingsLookupAdapter adapter.HoldingsLookupAdapter
}

func NewMetaproxyAvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config, metaproxyUrl string) (adapter.HoldingsLookupAdapter, error) {
	// TODO: holdingsParser
	holdingsParser := adapter.NewMarcHoldingsParser(nil)
	queryBuilder := adapter.NewQueryBuilderPqf(config.PqfMappings)
	a := &MetaproxyAvailabilityAdapter{
		holdingsLookupAdapter: adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{metaproxyUrl}, config.Address, queryBuilder, holdingsParser),
	}
	return a, nil
}

func (a *MetaproxyAvailabilityAdapter) Lookup(params adapter.HoldingLookupParams) ([]adapter.Holding, string, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
