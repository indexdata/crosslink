package adapter

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	HoldingsAdapter    string = "HOLDINGS_ADAPTER"
	SruUrl             string = "SRU_URL"
	HoldingsIsxnLookup string = "HOLDINGS_ISXN_LOOKUP"
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
		isxnVal, ok := cfg[HoldingsIsxnLookup]
		if !ok {
			return nil, fmt.Errorf("missing value for %s", HoldingsIsxnLookup)
		}
		// ideally this should be per-SRU server and not for all
		isxn := strings.ToLower(isxnVal) == "true"
		return CreateSruHoldingsLookupAdapter(http.DefaultClient, strings.Split(sruUrlVal, ","), isxn), nil
	}
	if holdingsAdapterVal == "mock" {
		return &MockHoldingsLookupAdapter{}, nil
	}
	return nil, fmt.Errorf("bad value for %s", HoldingsAdapter)
}
