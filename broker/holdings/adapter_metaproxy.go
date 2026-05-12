package holdings

import (
	"net/http"

	"github.com/indexdata/crosslink/directory"
)

type MetaproxyAvailabilityAdapter struct {
	holdingsLookupAdapter LookupAdapter
}

func NewMetaproxyAvailabilityAdapter(config directory.ZoomConfig, metaproxyUrl string, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser) (LookupAdapter, error) {
	a := &MetaproxyAvailabilityAdapter{
		holdingsLookupAdapter: CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{metaproxyUrl}, config.Address, queryBuilder, holdingsParser, "marcxml"),
	}
	return a, nil
}

func (a *MetaproxyAvailabilityAdapter) Lookup(params LookupParams) ([]Holding, string, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
