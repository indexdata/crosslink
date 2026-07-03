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

func getMetadataParser(config *directory.MetadataParserConfig) (MetadataParser, error) {
	if config == nil {
		return nil, nil
	}
	if config.Marc21 != nil {
		return NewMetadataParserMarc(*config.Marc21), nil
	}
	return nil, fmt.Errorf("availabilityConfig.metadataFormat only marc supported for now")
}

func getHoldingsParser(config *directory.ParserConfig) (HoldingsParser, error) {
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
	config := entry.HoldingsConfig
	if config == nil {
		return nil, nil // No holdings adapter for this peer
	}
	if c.mode == AvailabilityAdapterMock {
		return NewMockAvailabilityAdapter(*config)
	}
	holdingsParser, err := getHoldingsParser(config.ParserConfig)
	if err != nil {
		return nil, err
	}
	metadataParser, err := getMetadataParser(config.MetadataFormat)
	if err != nil {
		return nil, err
	}
	queryBuilder, err := NewQueryBuilderGen(config.QueryConfig)
	if err != nil {
		return nil, err
	}
	if config.Sru != nil {
		return NewSruAvailabilityAdapter(*config.Sru, queryBuilder, holdingsParser, metadataParser)
	}
	if config.Zoom != nil {
		switch c.mode {
		case AvailabilityAdapterMetaproxy:
			if c.metaproxyUrl == "" {
				return nil, fmt.Errorf("when using %s holdings adapter, %s environment variable must be set", AvailabilityAdapterMetaproxy, "METAPROXY_URL")
			}
			return NewMetaproxyAvailabilityAdapter(*config.Zoom, c.metaproxyUrl, queryBuilder, holdingsParser, metadataParser)
		case AvailabilityAdapterZoom:
			metadataParser, err := getMetadataParser(config.MetadataFormat)
			if err != nil {
				return nil, err
			}
			return NewZoomAvailabilityAdapter(*config.Zoom, queryBuilder, holdingsParser, metadataParser)
		default:
			return nil, fmt.Errorf("unsupported holdings adapter type: %s", c.mode)
		}
	}
	return nil, fmt.Errorf("must specify either sru or zoom properties for holdings adapter type")
}
