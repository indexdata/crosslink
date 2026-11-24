package catalog

import (
	"fmt"
	"net/http"
	"strings"

	dirapi "github.com/indexdata/crosslink/directory/api"
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

	HoldingsAdapterMock       string = "mock"
	HoldingsAdapterSru        string = "sru"
	HoldingsAdapterConsortium string = "consortium"
)

// HoldingsSource is one entry of the ordered HOLDINGS_ADAPTER list.
type HoldingsSource struct {
	// Name is the configured HOLDINGS_ADAPTER entry, e.g. "sru".
	Name string
	// Adapter is shared by all requesters. A nil Adapter means the adapter is
	// resolved per peer from its Directory catalog configuration.
	Adapter LookupAdapter
}

func getParserFormat(format string) (HoldingsParser, error) {
	switch format {
	case HoldingsFormatReservoir:
		return NewReservoirHoldingsParser(), nil
	case HoldingsFormatMarc21Plus1:
		return NewMarc21Plus1HoldingsParser(), nil
	case HoldingsFormatMarc:
		return NewMarcHoldingsParser(dirapi.MarcHoldingsParserConfig{}), nil
	case HoldingsFormatOpac:
		return NewOpacHoldingsParser(dirapi.OpacHoldingsParserConfig{}), nil
	default:
		return nil, fmt.Errorf("bad value for %s: %s", HoldingsFormat, format)
	}
}

// CreateLookupAdapterFromEnv parses the comma-separated HOLDINGS_ADAPTER list
// into holdings sources, preserving the configured order in which they are tried.
func CreateLookupAdapterFromEnv(cfg map[string]any) ([]HoldingsSource, error) {
	holdingsAdapterVal, ok := cfg[HoldingsAdapter].(string)
	if !ok {
		return nil, fmt.Errorf("missing value for %s", HoldingsAdapter)
	}
	var sources []HoldingsSource
	seen := map[string]bool{}
	for _, name := range strings.Split(holdingsAdapterVal, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("bad value for %s: empty entry in %q", HoldingsAdapter, holdingsAdapterVal)
		}
		if seen[name] {
			return nil, fmt.Errorf("bad value for %s: duplicate entry %q", HoldingsAdapter, name)
		}
		seen[name] = true
		source, err := createHoldingsSource(name, cfg)
		if err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}
	return sources, nil
}

func createHoldingsSource(name string, cfg map[string]any) (HoldingsSource, error) {
	switch name {
	case HoldingsAdapterConsortium:
		// consortium must be determined per-peer, so we can't create a single adapter for all peers
		return HoldingsSource{Name: name}, nil
	case HoldingsAdapterSru:
		adapter, err := createSruAdapterFromEnv(cfg)
		if err != nil {
			return HoldingsSource{}, err
		}
		return HoldingsSource{Name: name, Adapter: adapter}, nil
	case HoldingsAdapterMock:
		return HoldingsSource{Name: name, Adapter: &MockLookupShared{}}, nil
	default:
		return HoldingsSource{}, fmt.Errorf("bad value for %s: %s", HoldingsAdapter, name)
	}
}

func createSruAdapterFromEnv(cfg map[string]any) (LookupAdapter, error) {
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
	metadataParser := NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{})
	return CreateSruLookupAdapter(http.DefaultClient, strings.Split(sruURLVal, ","), "", &queryBuilder, parser, metadataParser, "marcxml"), nil
}
