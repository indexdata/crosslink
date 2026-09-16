package catalog

import (
	"context"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAdapterEmpty(t *testing.T) {
	creator := NewLookupAdapterCreator(LookupAdapterZoom, "")
	peer := ill_db.Peer{}
	aa, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestGetAdapterOtherNoConfig(t *testing.T) {
	creator := NewLookupAdapterCreator("other", "")
	peer := ill_db.Peer{}
	aa, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestParserNil(t *testing.T) {
	parser, err := getHoldingsParser(nil)
	assert.NoError(t, err)
	assert.IsType(t, &MarcHoldingsParser{}, parser)
}

func TestParserMissing(t *testing.T) {
	parserConfig := &dirapi.HoldingsParserConfig{}
	_, err := getHoldingsParser(parserConfig)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must set marc")
}

func TestParserMarc(t *testing.T) {
	parserConfig := &dirapi.HoldingsParserConfig{
		Marc: &dirapi.MarcHoldingsParserConfig{},
	}
	parser, err := getHoldingsParser(parserConfig)
	assert.NoError(t, err)
	assert.IsType(t, &MarcHoldingsParser{}, parser)
}

func TestParserOpac(t *testing.T) {
	parserConfig := &dirapi.HoldingsParserConfig{
		Opac: &dirapi.OpacHoldingsParserConfig{},
	}
	parser, err := getHoldingsParser(parserConfig)
	assert.NoError(t, err)
	assert.IsType(t, &OpacHoldingsParser{}, parser)
}

func TestGetAdapterBadParser(t *testing.T) {
	creator := NewLookupAdapterCreator(LookupAdapterZoom, "")
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Zoom: &dirapi.ZoomConfig{
					Address: "a",
				},
				HoldingsFormat: &dirapi.HoldingsParserConfig{
					Marc: &dirapi.MarcHoldingsParserConfig{},
					Opac: &dirapi.OpacHoldingsParserConfig{},
				},
			},
		},
	}
	adapter, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	require.ErrorContains(t, err, "exactly one parser")
	require.Nil(t, adapter)
}

func TestGetAdapterEmptyHoldingsUsesMarcDefaults(t *testing.T) {
	creator := NewLookupAdapterCreator(LookupAdapterZoom, "")
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Sru:            &dirapi.SruConfig{Address: "https://catalog.example/sru"},
				HoldingsFormat: &dirapi.HoldingsParserConfig{},
			},
		},
	}
	adapter, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	require.NoError(t, err)
	require.IsType(t, &SruLookupAdapter{}, adapter)
	sru := adapter.(*SruLookupAdapter)
	require.Equal(t, NewMarcHoldingsParser(dirapi.MarcHoldingsParserConfig{}), sru.holdingsParser)
	require.Equal(t, "marcxml", sru.recordSchema)
}

func TestGetAdapterOtherWithConfig(t *testing.T) {
	creator := NewLookupAdapterCreator("other", "")
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Zoom: &dirapi.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	_, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported lookup adapter type: other")
}

func TestGetAdapterMetadataOnly(t *testing.T) {
	creator := NewLookupAdapterCreator("zoom", "")
	mode := dirapi.Merge
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{MetadataUpdateMode: &mode},
		},
	}
	aa, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestGetAdapterMock(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Zoom: &dirapi.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewLookupAdapterCreator(LookupAdapterMock, "")
	aa, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.NoError(t, err)
	assert.IsType(t, &MockLookupAdapter{}, aa)
}

func TestGetAdapterZoom(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Zoom: &dirapi.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewLookupAdapterCreator(LookupAdapterZoom, "")
	aa, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	if !cgoEnabled() {
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires cgo")
		assert.Nil(t, aa)
	} else {
		assert.NoError(t, err)
		assert.IsType(t, &ZoomLookupAdapter{}, aa)
	}
}

func TestGetAdapterMetaproxy(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Zoom: &dirapi.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewLookupAdapterCreator(LookupAdapterMetaproxy, "http://metaproxy.indexdata.com")
	aa, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.NoError(t, err)
	assert.IsType(t, &MetaproxyLookupAdapter{}, aa)
}

func TestGetAdapterMetaproxyMissingProxy(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Zoom: &dirapi.ZoomConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewLookupAdapterCreator(LookupAdapterMetaproxy, "")
	_, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "METAPROXY_URL")
}

func TestGetAdapterSRU(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: dirapi.Entry{
			CatalogConfig: &dirapi.CatalogConfig{
				Sru: &dirapi.SruConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewLookupAdapterCreator(LookupAdapterZoom, "")
	aa, err := creator.GetAdapter(common.CreateExtCtxWithArgs(context.Background(), nil), peer)
	assert.NoError(t, err)
	assert.IsType(t, &SruLookupAdapter{}, aa)
}
