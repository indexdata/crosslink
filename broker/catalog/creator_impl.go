package catalog

import (
	"fmt"

	"github.com/indexdata/crosslink/broker/ill_db"
	dirapi "github.com/indexdata/crosslink/directory/api"
)

const (
	LookupAdapterZoom      string = "zoom"      // yaz zoom adapter
	LookupAdapterMock      string = "mock"      // mock adapter for testing
	LookupAdapterMetaproxy string = "metaproxy" // metaproxy adapter (x-target)
)

type LookupAdapterCreatorImpl struct {
	mode         string
	metaproxyUrl string
}

func NewLookupAdapterCreator(mode string, metaproxyUrl string) LookupAdapterCreator {
	return &LookupAdapterCreatorImpl{
		mode:         mode,
		metaproxyUrl: metaproxyUrl,
	}
}

func getMetadataParser(config *dirapi.MetadataParserConfig) (MetadataParser, error) {
	if config == nil {
		return NewMetadataParserMarc(dirapi.MarcMetadataParserConfig{}), nil
	}
	if config.Marc21 != nil {
		return NewMetadataParserMarc(*config.Marc21), nil
	}
	return nil, fmt.Errorf("catalogConfig.metadataFormat must set marc21 (only marc21 is supported for now)")
}

func getHoldingsParser(config *dirapi.HoldingsParserConfig) (HoldingsParser, error) {
	if config == nil {
		return NewMarcHoldingsParser(dirapi.MarcHoldingsParserConfig{}), nil // default to marc parser
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
	return nil, fmt.Errorf("catalogConfig.holdingsFormat must set marc, opac, reservoir, or marc21plus1 properties")
}

func (c *LookupAdapterCreatorImpl) GetAdapter(peer ill_db.Peer) (LookupAdapter, error) {
	entry := peer.CustomData
	config := entry.CatalogConfig
	if config == nil {
		return nil, nil // No lookup adapter for this peer
	}
	if c.mode == LookupAdapterMock {
		return NewMockLookupAdapter(*config)
	}
	holdingsParser, err := getHoldingsParser(config.HoldingsFormat)
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
		return NewSruLookupAdapter(*config.Sru, queryBuilder, holdingsParser, metadataParser)
	}
	if config.Zoom != nil {
		switch c.mode {
		case LookupAdapterMetaproxy:
			if c.metaproxyUrl == "" {
				return nil, fmt.Errorf("when using %s lookup adapter, %s environment variable must be set", LookupAdapterMetaproxy, "METAPROXY_URL")
			}
			return NewMetaproxyLookupAdapter(*config.Zoom, c.metaproxyUrl, queryBuilder, holdingsParser, metadataParser)
		case LookupAdapterZoom:
			return NewZoomLookupAdapter(*config.Zoom, queryBuilder, holdingsParser, metadataParser)
		default:
			return nil, fmt.Errorf("unsupported lookup adapter type: %s", c.mode)
		}
	}
	return nil, fmt.Errorf("must specify either sru or zoom properties for lookup adapter type")
}
