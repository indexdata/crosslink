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

func NewSruAvailabilityAdapter(ctx common.ExtendedContext, config directory.Z3950Config) (adapter.HoldingsLookupAdapter, error) {
	// TODO: holdingsParser based on config
	holdingsParser := adapter.NewMarcHoldingsParser(nil)
	a := &SruAvailabilityAdapter{
		holdingsLookupAdapter: adapter.CreateSruHoldingsLookupAdapter(http.DefaultClient, []string{config.Address}, "", true, holdingsParser),
	}
	return a, nil
}

func (a *SruAvailabilityAdapter) Lookup(params adapter.HoldingLookupParams) ([]adapter.Holding, string, error) {
	return a.holdingsLookupAdapter.Lookup(params)
}
