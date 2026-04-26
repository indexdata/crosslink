package adapter

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	HoldingsAdapter string = "HOLDINGS_ADAPTER"
	HoldingsSruURL  string = "HOLDINGS_SRU_URL"
)

func CreateHoldingsLookupAdapter(cfg map[string]string) (HoldingsLookupAdapter, error) {
	holdingsAdapterVal, ok := cfg[HoldingsAdapter]
	if !ok {
		return nil, fmt.Errorf("missing value for %s", HoldingsAdapter)
	}
	if holdingsAdapterVal == "sru" {
		sruURLVal, ok := cfg[HoldingsSruURL]
		if !ok {
			return nil, fmt.Errorf("missing value for %s", HoldingsSruURL)
		}
		return CreateSruHoldingsLookupAdapter(http.DefaultClient, strings.Split(sruURLVal, ",")), nil
	}
	if holdingsAdapterVal == "mock" {
		return &MockHoldingsLookupAdapter{}, nil
	}
	return nil, fmt.Errorf("bad value for %s", HoldingsAdapter)
}
