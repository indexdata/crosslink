package availability

import (
	"fmt"
	"strings"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
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

func getParser(config *directory.ParserConfig) (adapter.HoldingsParser, error) {
	if config == nil {
		return adapter.NewMarcHoldingsParser(directory.MarcParserConfig{}), nil // default to marc parser
	}
	if config.Marc != nil {
		return adapter.NewMarcHoldingsParser(*config.Marc), nil
	}
	if config.Opac != nil {
		return adapter.NewOpacHoldingsParser(*config.Opac), nil
	}
	return nil, fmt.Errorf("bad value for availability parser type")
}

func (c *AvailabilityCreatorImpl) GetAdapter(ctx common.ExtendedContext, peer ill_db.Peer) (adapter.HoldingsLookupAdapter, error) {
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
	aType := directory.Z3950
	if config.Type != nil {
		aType = *config.Type
	} else {
		if strings.HasPrefix(config.Address, "http:/") || strings.HasPrefix(config.Address, "https:/") {
			aType = directory.SRU
		}
	}
	switch aType {
	case directory.SRU:
		return NewSruAvailabilityAdapter(ctx, *config, holdingsParser)
	case directory.Z3950:
		// Z39.50 can be supported by Metaproxy or yaz zoom adapter, but not both at the same time
		if c.mode == AvailabilityAdapterMetaproxy {
			if c.metaproxyUrl == "" {
				return nil, fmt.Errorf("when using %s availability adapter, %s environment variable must be set", AvailabilityAdapterMetaproxy, "METAPROXY_URL")
			}
			return NewMetaproxyAvailabilityAdapter(ctx, *config, c.metaproxyUrl, holdingsParser)
		}
		if c.mode == AvailabilityAdapterZoom {
			return NewZoomAvailabilityAdapter(ctx, *config, holdingsParser)
		}
	}
	return nil, fmt.Errorf("bad value for availability adapter type: %s", aType)
}
