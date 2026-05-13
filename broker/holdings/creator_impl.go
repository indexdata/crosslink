package holdings

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
)

const (
	AvailabilityAdapterZoom      string = "zoom"      // yaz zoom adapter
	AvailabilityAdapterMock      string = "mock"      // mock adapter for testing
	AvailabilityAdapterMetaproxy string = "metaproxy" // metaproxy adapter (x-target)
)

type AvailabilityCreatorImpl struct {
	mode         string
	metaproxyUrl string
}

func NewAvailabilityCreator(mode string, metaproxyUrl string) AvailabilityCreator {
	return &AvailabilityCreatorImpl{
		mode:         mode,
		metaproxyUrl: metaproxyUrl,
	}
}

func getParser(config *directory.ParserConfig) (HoldingsParser, error) {
	if config == nil {
		return NewMarcHoldingsParser(directory.MarcParserConfig{}), nil // default to marc parser
	}
	if config.Marc != nil {
		return NewMarcHoldingsParser(*config.Marc), nil
	}
	if config.Opac != nil {
		return NewOpacHoldingsParser(*config.Opac), nil
	}
	if config.Reservoir != nil {
		return NewReservoirHoldingsParser(), nil
	}
	if config.Marc21plus1 != nil {
		return NewMarc21Plus1HoldingsParser(), nil
	}
	return nil, fmt.Errorf("availabilityConfig.parserConfig must set marc, opac, reservoir, or marc21plus1 properties")
}

func (c *AvailabilityCreatorImpl) GetAdapter(peer ill_db.Peer) (LookupAdapter, error) {
	entry := peer.CustomData
	config := entry.AvailabilityConfig
	if config == nil {
		return nil, nil // No availability adapter for this peer
	}
	if c.mode == AvailabilityAdapterMock {
		return NewMockAvailabilityAdapter(*config)
	}
	holdingsParser, err := getParser(config.ParserConfig)
	if err != nil {
		return nil, err
	}
	queryBuilder, err := NewQueryBuilderGen(config.QueryConfig)
	if err != nil {
		return nil, err
	}
	if config.Sru != nil {
		return NewSruAvailabilityAdapter(*config.Sru, queryBuilder, holdingsParser)
	}
	if config.Zoom != nil {
		switch c.mode {
		case AvailabilityAdapterMetaproxy:
			if c.metaproxyUrl == "" {
				return nil, fmt.Errorf("when using %s availability adapter, %s environment variable must be set", AvailabilityAdapterMetaproxy, "METAPROXY_URL")
			}
			return NewMetaproxyAvailabilityAdapter(*config.Zoom, c.metaproxyUrl, queryBuilder, holdingsParser)
		case AvailabilityAdapterZoom:
			return NewZoomAvailabilityAdapter(*config.Zoom, queryBuilder, holdingsParser)
		default:
			return nil, fmt.Errorf("unsupported availability adapter type: %s", c.mode)
		}
	}
	return nil, fmt.Errorf("must specify either sru or zoom properties for availability adapter type")
}
