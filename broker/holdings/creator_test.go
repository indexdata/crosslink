package holdings

import (
	"testing"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestGetAdapterEmpty(t *testing.T) {
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	peer := ill_db.Peer{}
	aa, err := creator.GetAdapter(peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestGetAdapterOtherNoConfig(t *testing.T) {
	creator := NewAvailabilityCreator("other", "")
	peer := ill_db.Peer{}
	aa, err := creator.GetAdapter(peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestParserNil(t *testing.T) {
	parser, err := getHoldingsParser(nil)
	assert.NoError(t, err)
	assert.IsType(t, &MarcHoldingsParser{}, parser)
}

func TestParserMissing(t *testing.T) {
	parserConfig := &directory.ParserConfig{}
	_, err := getHoldingsParser(parserConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must set marc")
}

func TestParserMarc(t *testing.T) {
	parserConfig := &directory.ParserConfig{
		Marc: &directory.MarcParserConfig{},
	}
	parser, err := getHoldingsParser(parserConfig)
	assert.NoError(t, err)
	assert.IsType(t, &MarcHoldingsParser{}, parser)
}

func TestParserOpac(t *testing.T) {
	parserConfig := &directory.ParserConfig{
		Opac: &directory.OpacParserConfig{},
	}
	parser, err := getHoldingsParser(parserConfig)
	assert.NoError(t, err)
	assert.IsType(t, &OpacHoldingsParser{}, parser)
}

func TestGetAdapterBadParser(t *testing.T) {
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Zoom: &directory.ZoomConfig{
					Address: "a",
				},
				ParserConfig: &directory.ParserConfig{},
			},
		},
	}
	_, err := creator.GetAdapter(peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must set marc")
}

func TestGetAdapterOtherWithConfig(t *testing.T) {
	creator := NewAvailabilityCreator("other", "")
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Zoom: &directory.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	_, err := creator.GetAdapter(peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported holdings adapter type: other")
}

func TestGetAdapterMissingProperties(t *testing.T) {
	creator := NewAvailabilityCreator("zoom", "")
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{},
		},
	}
	_, err := creator.GetAdapter(peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify either sru or zoom properties")
}

func TestGetAdapterMock(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Zoom: &directory.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterMock, "")
	aa, err := creator.GetAdapter(peer)
	assert.NoError(t, err)
	assert.IsType(t, &MockAvailabilityAdapter{}, aa)
}

func TestGetAdapterZoom(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Zoom: &directory.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	aa, err := creator.GetAdapter(peer)
	if !cgoEnabled() {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires cgo")
		assert.Nil(t, aa)
	} else {
		assert.NoError(t, err)
		assert.IsType(t, &ZoomAvailabilityAdapter{}, aa)
	}
}

func TestGetAdapterMetaproxy(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Zoom: &directory.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterMetaproxy, "http://metaproxy.indexdata.com")
	aa, err := creator.GetAdapter(peer)
	assert.NoError(t, err)
	assert.IsType(t, &MetaproxyAvailabilityAdapter{}, aa)
}

func TestGetAdapterMetaproxyMissingProxy(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Zoom: &directory.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterMetaproxy, "")
	_, err := creator.GetAdapter(peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "METAPROXY_URL")
}

func TestGetAdapterSRU(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			HoldingsConfig: &directory.HoldingsConfig{
				Sru: &directory.SruConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	aa, err := creator.GetAdapter(peer)
	assert.NoError(t, err)
	assert.IsType(t, &SruHoldingsLookupAdapter{}, aa)
}
