package availability

import (
	"context"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/directory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestGetAdapterNoIllRepo(t *testing.T) {
	creator := NewAvailabilityCreator(nil, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	peers, err := creator.GetAdapter(ctx, symbol)
	assert.NoError(t, err)
	assert.Nil(t, peers)
}

func TestGetAdapterGetCachedByPeersByPeersFail(t *testing.T) {
	illRepo := &MockIllRepo{}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{}, "", assert.AnError)
	creator := NewAvailabilityCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	_, err := creator.GetAdapter(ctx, symbol)
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())
}

func TestGetAdapterNotFound(t *testing.T) {
	illRepo := &MockIllRepo{}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{}, "", nil)
	creator := NewAvailabilityCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	adapter, err := creator.GetAdapter(ctx, symbol)
	assert.NoError(t, err)
	assert.Nil(t, adapter)
}

func TestGetAdapterZ3950(t *testing.T) {
	illRepo := &MockIllRepo{}
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			Z3950Config: &directory.Z3950Config{
				Address: "z3950.indexdata.com/marc",
			},
		},
	}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{peer}, "", nil)
	creator := NewAvailabilityCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	adapter, err := creator.GetAdapter(ctx, symbol)
	assert.NoError(t, err)
	if adapter == nil {
		assert.False(t, cgoEnabled(), "Expected no adapter when cgo is disabled")
		return
	}
	assert.True(t, cgoEnabled(), "Expected adapter when cgo is enabled")
	assert.IsType(t, &Z3950AvailabilityAdapter{}, adapter)
}

type MockIllRepo struct {
	mock.Mock
	ill_db.PgIllRepo
}

func (r *MockIllRepo) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryLookupAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	args := r.Called(symbols)
	return args.Get(0).([]ill_db.Peer), args.String(1), args.Error(2)
}
