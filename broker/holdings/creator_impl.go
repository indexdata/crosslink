package holdings

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
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
	return nil, fmt.Errorf("holdingsConfig.metadataFormat only marc supported for now")
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
	return nil, fmt.Errorf("holdingsConfig.parserConfig must set marc, opac, reservoir, or marc21plus1 properties")
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
			return NewZoomAvailabilityAdapter(*config.Zoom, queryBuilder, holdingsParser, metadataParser)
		default:
			return nil, fmt.Errorf("unsupported holdings adapter type: %s", c.mode)
		}
	}
	return nil, fmt.Errorf("must specify either sru or zoom properties for holdings adapter type")
}

func LookupParamsFromBibliographicInfo(info iso18626.BibliographicInfo, serviceInfo *iso18626.ServiceInfo) LookupParams {
	var serviceType string
	if serviceInfo != nil {
		serviceType = string(serviceInfo.ServiceType)
	}
	params := LookupParams{
		Identifier:  info.SupplierUniqueRecordId,
		Title:       info.Title,
		ServiceType: serviceType,
	}
	for _, id := range info.BibliographicItemId {
		switch strings.TrimSpace(id.BibliographicItemIdentifierCode.Text) {
		case "ISBN":
			params.Isbn = id.BibliographicItemIdentifier
		case "ISSN":
			params.Issn = id.BibliographicItemIdentifier
		}
	}
	return params
}
