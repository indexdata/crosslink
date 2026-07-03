package holdings

import (
	"net/http"

	"github.com/indexdata/crosslink/directory"
)

type MetaproxyAvailabilityAdapter struct {
	holdingsLookupAdapter LookupAdapter
}

func NewMetaproxyAvailabilityAdapter(config directory.ZoomConfig, metaproxyUrl string, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	a := &MetaproxyAvailabilityAdapter{
		holdingsLookupAdapter: CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{metaproxyUrl}, config.Address, queryBuilder, holdingsParser, metadataParser, "marcxml"),
	}
	return a, nil
}

func (a *MetaproxyAvailabilityAdapter) HoldingsLookup(params LookupParams) ([]Holding, string, error) {
	return a.holdingsLookupAdapter.HoldingsLookup(params)
}

func (a *MetaproxyAvailabilityAdapter) MetadataLookup(params LookupParams) (Metadata, error) {
	return a.holdingsLookupAdapter.MetadataLookup(params)
}
