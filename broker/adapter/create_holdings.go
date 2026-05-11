package adapter

import (
	"fmt"
	"net/http"
	"strings"
)

const (
	HoldingsAdapter    string = "HOLDINGS_ADAPTER"
	HoldingsSruURL     string = "HOLDINGS_SRU_URL"
	HoldingsIsxnLookup string = "HOLDINGS_ISXN_LOOKUP"
	HoldingsFormat     string = "HOLDINGS_FORMAT"
)

func getParser(format string) (HoldingsParser, error) {
	switch format {
	case "reservoir":
		return &ReservoirHoldingsParser{}, nil
	case "MARC-21plus-1":
		return &Marc21Plus1HoldingsParser{}, nil
	default:
		return nil, fmt.Errorf("unsupported holdings format: %s", format)
	}
}

func CreateHoldingsLookupAdapter(cfg map[string]any) (LookupAdapter, error) {
	holdingsAdapterVal, ok := cfg[HoldingsAdapter].(string)
	if !ok {
		return nil, fmt.Errorf("missing value for %s", HoldingsAdapter)
	}
	if holdingsAdapterVal == "sru" {
		sruURLVal, ok := cfg[HoldingsSruURL].(string)
		if !ok {
			return nil, fmt.Errorf("missing value for %s", HoldingsSruURL)
		}
		_, ok = cfg[HoldingsIsxnLookup]
		if !ok {
			return nil, fmt.Errorf("missing value for %s", HoldingsIsxnLookup)
		}
		// ideally this should be per-SRU server and not for all
		isxnLookup, ok := cfg[HoldingsIsxnLookup].(bool)
		if !ok {
			return nil, fmt.Errorf("invalid value for %s: %v", HoldingsIsxnLookup, isxnLookup)
		}
		queryBuilder := QueryBuilderIsxn{isxn: isxnLookup}
		parser, err := getParser(cfg[HoldingsFormat].(string))
		if err != nil {
			return nil, err
		}
		return CreateSruHoldingsLookupAdapter(http.DefaultClient, strings.Split(sruURLVal, ","), "", &queryBuilder, parser, "marcxml"), nil
	}
	if holdingsAdapterVal == "mock" {
		return &MockHoldingsLookupAdapter{}, nil
	}
	return nil, fmt.Errorf("bad value for %s", HoldingsAdapter)
}
