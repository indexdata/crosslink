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
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	peer := ill_db.Peer{}
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.Nil(t, aa)
}

func TestGetAdapterOther(t *testing.T) {
	creator := NewAvailabilityCreator("other")
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	peer := ill_db.Peer{}
	_, err := creator.GetAdapter(ctx, peer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "bad value for")
}

func TestGetAdapterMock(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			Z3950Config: &directory.Z3950Config{
				Address: "z3950.indexdata.com/marc",
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterMock)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	assert.IsType(t, &MockAvailabilityAdapter{}, aa)
}

func TestGetAdapterZ3950(t *testing.T) {
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			Z3950Config: &directory.Z3950Config{
				Address: "z3950.indexdata.com/marc",
			},
		},
	}
	creator := NewAvailabilityCreator(AvailabilityAdapterZoom)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	aa, err := creator.GetAdapter(ctx, peer)
	assert.NoError(t, err)
	if aa == nil {
		assert.False(t, cgoEnabled(), "Expected no adapter when cgo is disabled")
		return
	}
	assert.True(t, cgoEnabled(), "Expected adapter when cgo is enabled")
	assert.IsType(t, &Z3950AvailabilityAdapter{}, aa)
}
