package holdings

import (
	"net/http"

	"github.com/indexdata/crosslink/directory"
)

type SruAvailabilityAdapter struct {
	holdingsLookupAdapter LookupAdapter
}

func NewSruAvailabilityAdapter(config directory.SruConfig, queryBuilder LookupQueryBuilder, holdingsParser HoldingsParser) (LookupAdapter, error) {
	var recordSchema string
	if config.RecordSchema != nil {
		recordSchema = *config.RecordSchema
	}
	if recordSchema == "" {
		recordSchema = "marcxml" // default to marcxml if not specified
	}
	a := &SruAvailabilityAdapter{
		holdingsLookupAdapter: CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{config.Address}, "", queryBuilder, holdingsParser, recordSchema),
	}
	return a, nil
}

func (a *SruAvailabilityAdapter) Lookup(params LookupParams) ([]Holding, string, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
