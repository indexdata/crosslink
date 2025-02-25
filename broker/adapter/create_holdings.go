package adapter

import (
	"fmt"
	"net/http"
)

const (
	HoldingsAdapter string = "HOLDINGS_ADAPTER"
	SruUrl          string = "SRU_URL"
)

func CreateHoldingsLookupAdapter(cfg map[string]string) (HoldingsLookupAdapter, error) {
	holdingsAdapterVal, ok := cfg[HoldingsAdapter]
	if !ok {
		return nil, fmt.Errorf("missing value for %s", HoldingsAdapter)
	}
	if holdingsAdapterVal == "sru" {
		sruUrlVal, ok := cfg[SruUrl]
		if !ok {
			return nil, fmt.Errorf("missing value for %s", SruUrl)
		}
		return CreateSruHoldingsLookupAdapter(http.DefaultClient, sruUrlVal), nil
	}
	if holdingsAdapterVal == "mock" {
		return &MockHoldingsLookupAdapter{}, nil
	}
	return nil, fmt.Errorf("bad value for %s", HoldingsAdapter)
}
