package catalog

import (
	"net/http"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

type MetaproxyLookupAdapter struct {
	holdingsLookupAdapter LookupAdapter
}

func NewMetaproxyLookupAdapter(config dirapi.ZoomConfig, metaproxyUrl string, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	a := &MetaproxyLookupAdapter{
		holdingsLookupAdapter: CreateSruLookupAdapter(http.DefaultClient, []string{metaproxyUrl}, config.Address, queryBuilder, holdingsParser, metadataParser, "marcxml"),
	}
	return a, nil
}

func (a *MetaproxyLookupAdapter) Lookup(params LookupParams) (LookupResult, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
