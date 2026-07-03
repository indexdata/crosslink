package holdings

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/directory"
)

const (
	HoldingsAdapter           string = "HOLDINGS_ADAPTER"
	HoldingsSruURL            string = "HOLDINGS_SRU_URL"
	HoldingsIsxnLookup        string = "HOLDINGS_ISXN_LOOKUP"
	HoldingsFormat            string = "HOLDINGS_FORMAT"
	HoldingsFormatReservoir   string = "reservoir"
	HoldingsFormatMarc21Plus1 string = "MARC-21plus-1"
	HoldingsFormatMarc        string = "marc"
	HoldingsFormatOpac        string = "opac"
)

func getParserFormat(format string) (HoldingsParser, error) {
	switch format {
	case HoldingsFormatReservoir:
		return NewReservoirHoldingsParser(), nil
	case HoldingsFormatMarc21Plus1:
		return NewMarc21Plus1HoldingsParser(), nil
	case HoldingsFormatMarc:
		return NewMarcHoldingsParser(directory.MarcParserConfig{}), nil
	case HoldingsFormatOpac:
		return NewOpacHoldingsParser(directory.OpacParserConfig{}), nil
	default:
		return nil, fmt.Errorf("bad value for %s: %s", HoldingsFormat, format)
	}
}

func CreateHoldingsLookupShared(cfg map[string]any) (LookupAdapter, error) {
	holdingsAdapterVal, ok := cfg[HoldingsAdapter].(string)
	if !ok {
		return nil, fmt.Errorf("missing value for %s", HoldingsAdapter)
	}
	if holdingsAdapterVal == "consortium" {
		// consortium must be determined per-peer, so we can't create a single adapter for all peers
		return nil, nil
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
			return nil, fmt.Errorf("invalid value for %s: %v", HoldingsIsxnLookup, cfg[HoldingsIsxnLookup])
		}
		queryBuilder := QueryBuilderIsxn{isxn: isxnLookup}
		format, ok := cfg[HoldingsFormat].(string)
		if !ok {
			return nil, fmt.Errorf("missing value for %s", HoldingsFormat)
		}
		parser, err := getParserFormat(format)
		if err != nil {
			return nil, err
		}
		return CreateSruHoldingsLookupAdapter(http.DefaultClient, strings.Split(sruURLVal, ","), "", &queryBuilder, parser, nil, "marcxml"), nil
	}
	if holdingsAdapterVal == "mock" {
		return &MockHoldingsLookupAdapter{}, nil
	}
	return nil, fmt.Errorf("bad value for %s", HoldingsAdapter)
}
