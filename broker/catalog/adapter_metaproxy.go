package catalog

import (
	"net/http"

	dirapi "github.com/indexdata/crosslink/directory/api"
)

type MetaproxyLookupAdapter struct {
	holdingsLookupAdapter LookupAdapter
}

func NewMetaproxyLookupAdapter(config dirapi.ZoomConfig, metaproxyUrl string, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser, metadataParser MetadataParser) (LookupAdapter, error) {
	schema := "marcxml"
	if config.Options != nil && (*config.Options)["preferredRecordSyntax"] == "opac" {
		schema = "opac"
	}
	a := &MetaproxyLookupAdapter{
		holdingsLookupAdapter: CreateSruLookupAdapter(http.DefaultClient, []string{metaproxyUrl}, config.Address, queryBuilder, holdingsParser, metadataParser, schema),
	}
	return a, nil
}

func (a *MetaproxyLookupAdapter) Lookup(params LookupParams) (LookupResult, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
