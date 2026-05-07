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
				Address: "z3950.indexdata.com/marc",
			},
		},
	}
	_, err := creator.GetAdapter(ctx, peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad value for")
}

func TestGetAdapterMock(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Address: "z3950.indexdata.com/marc",
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterMock, "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.IsType(t, &MockAvailabilityAdapter{}, aa)
}

func TestGetAdapterZ3950WithoutType(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Address: "z3950.indexdata.com/marc",
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

func TestGetAdapterZ3950WithType(t *testing.T) {
	dtype := directory.Z3950
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Type:    &dtype,
				Address: "https://z3950.indexdata.com/marc", // wouold be treated as SRU if type was not specified
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

func TestGetAdapterSRU(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			AvailabilityConfig: &directory.AvailabilityConfig{
				Address: "http://sru.indexdata.com/marc",
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom, "")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.IsType(t, &SruAvailabilityAdapter{}, aa)
}
