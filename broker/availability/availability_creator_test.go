package availability

import (
	"context"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
)

func TestGetAdapterEmpty(t *testing.T) {
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	peer := ill_db.Peer{}
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestGetAdapterOtherNoConfig(t *testing.T) {
	creator := NewAvailabilityCreator("other", "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	peer := ill_db.Peer{}
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestGetAdapterOtherWithConfig(t *testing.T) {
	creator := NewAvailabilityCreator("other", "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Z3950: &directory.Z3950Config{
					Address: "a",
				},
			},
		},
	}
	_, err := creator.GetAdapter(ctx, peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported availability adapter type: other")
}

func TestGetAdapterMissingProperties(t *testing.T) {
	creator := NewAvailabilityCreator("zoom", "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{},
		},
	}
	_, err := creator.GetAdapter(ctx, peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "must specify either sru or z3950 properties")
}

func TestGetAdapterMock(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Z3950: &directory.Z3950Config{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterMock, "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.IsType(t, &MockAvailabilityAdapter{}, aa)
}

func TestGetAdapterZoom(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Z3950: &directory.Z3950Config{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
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
			AvailabilityConfig: &directory.AvailabilityConfig{
				Z3950: &directory.Z3950Config{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterMetaproxy, "http://metaproxy.indexdata.com")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.IsType(t, &MetaproxyAvailabilityAdapter{}, aa)
}

func TestGetAdapterSRU(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Sru: &directory.SruConfig{
					Address: "a",
				},
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.IsType(t, &SruAvailabilityAdapter{}, aa)
}
