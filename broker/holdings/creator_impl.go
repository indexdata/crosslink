package holdings

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/metadataupdate"
	"github.com/indexdata/crosslink/directory"
	"github.com/indexdata/crosslink/iso18626"
)

const (
	LookupHintIdentifier = "identifier"
	LookupHintIsxn       = "isxn"
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
	config := entry.HoldingsConfig
	if config == nil {
		return nil, nil // No holdings adapter for this peer
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
				return nil, fmt.Errorf("when using %s holdings adapter, %s environment variable must be set", AvailabilityAdapterMetaproxy, "METAPROXY_URL")
			}
			return NewMetaproxyAvailabilityAdapter(*config.Zoom, c.metaproxyUrl, queryBuilder, holdingsParser)
		case AvailabilityAdapterZoom:
			return NewZoomAvailabilityAdapter(*config.Zoom, queryBuilder, holdingsParser)
		default:
			return nil, fmt.Errorf("unsupported holdings adapter type: %s", c.mode)
		}
	}
	return nil, fmt.Errorf("must specify either sru or zoom properties for holdings adapter type")
}

func GetMetadataSettings(entry directory.Entry) MetadataSettings {
	settings := MetadataSettings{
		Mode:   directory.None,
		Format: metadataupdate.DefaultMetadataFormat,
	}
	if entry.HoldingsConfig == nil {
		return settings
	}
	if entry.HoldingsConfig.MetadataUpdateMode != nil {
		settings.Mode = directory.MetadataUpdateMode(strings.ToLower(strings.TrimSpace(string(*entry.HoldingsConfig.MetadataUpdateMode))))
	}
	if entry.HoldingsConfig.MetadataFormat != nil {
		settings.Format = ResolveMetadataFormat(string(*entry.HoldingsConfig.MetadataFormat))
	}
	return settings
}

func ResolveMetadataUpdateMode(mode string, hint string) directory.MetadataUpdateMode {
	resolved := directory.None
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(directory.Replace):
		resolved = directory.Replace
	case string(directory.Merge):
		resolved = directory.Merge
	case string(directory.Auto):
		resolved = directory.Auto
	case string(directory.None), "":
		resolved = directory.None
	default:
		resolved = directory.None
	}
	if resolved != directory.Auto {
		return resolved
	}
	if hint == LookupHintIdentifier {
		return directory.Replace
	}
	if hint == LookupHintIsxn {
		return directory.Merge
	}
	return directory.None
}

func ResolveMetadataFormat(format string) directory.MetadataFormat {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case string(directory.Marc21), "":
		return directory.Marc21
	default:
		return directory.MetadataFormat(strings.ToLower(strings.TrimSpace(format)))
	}
}

func LookupHintFromParams(params LookupParams) string {
	if strings.TrimSpace(params.Identifier) != "" {
		return LookupHintIdentifier
	}
	if strings.TrimSpace(params.Isbn) != "" || strings.TrimSpace(params.Issn) != "" {
		return LookupHintIsxn
	}
	return ""
}

func LookupParamsFromBibliographicInfo(info iso18626.BibliographicInfo, serviceType string) LookupParams {
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
