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

func NewSruAvailabilityAdapter(ctx common.ExtendedContext, config directory.SruConfig, queryBuilder adapter.HoldingsQueryBuilder, holdingsParser adapter.HoldingsParser) (adapter.HoldingsLookupAdapter, error) {
	var recordSchema string
	if config.RecordSchema != nil {
		recordSchema = *config.RecordSchema
	}
	if recordSchema == "" {
		recordSchema = "marcxml" // default to marcxml if not specified
	}
	a := &SruAvailabilityAdapter{
		holdingsLookupAdapter: adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{config.Address}, "", queryBuilder, holdingsParser, recordSchema),
	}
	return a, nil
}

func (a *SruAvailabilityAdapter) Lookup(params adapter.HoldingLookupParams) ([]adapter.Holding, string, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
