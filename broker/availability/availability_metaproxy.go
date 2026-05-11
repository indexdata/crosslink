package availability

import (
	"net/http"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/directory"
)

type MetaproxyAvailabilityAdapter struct {
	holdingsLookupAdapter adapter.LookupAdapter
}

func NewMetaproxyAvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config, metaproxyUrl string, queryBuilder adapter.LookupQueryBuilder, holdingsParser adapter.HoldingsParser) (adapter.LookupAdapter, error) {
	a := &MetaproxyAvailabilityAdapter{
		holdingsLookupAdapter: adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{metaproxyUrl}, config.Address, queryBuilder, holdingsParser, "marcxml"),
	}
	return a, nil
}

func (a *MetaproxyAvailabilityAdapter) Lookup(params adapter.LookupParams) ([]adapter.Holding, string, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
