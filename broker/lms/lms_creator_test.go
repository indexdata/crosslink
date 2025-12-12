package lms

import (
	"context"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestGetAdapterMissingSymbol(t *testing.T) {
	illRepo := &MockIllRepo{}
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	var missingSymbol pgtype.Text
	_, err := creator.GetAdapter(ctx, missingSymbol)
	assert.Error(t, err)
	assert.Equal(t, "missing requester symbol", err.Error())
}

func TestGetAdapterGetCachedByPeersByPeersFail(t *testing.T) {
	illRepo := &MockIllRepo{}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{}, "", assert.AnError)
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := pgtype.Text{String: "TEST", Valid: true}
	_, err := creator.GetAdapter(ctx, symbol)
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())
}

func TestGetAdapterNoPeers(t *testing.T) {
	illRepo := &MockIllRepo{}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{}, "", nil)
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := pgtype.Text{String: "TEST", Valid: true}
	LmsAdapter, err := creator.GetAdapter(ctx, symbol)
	assert.NoError(t, err)
	assert.IsType(t, &LmsAdapterMock{}, LmsAdapter)
}

func TestGetAdapterNcip(t *testing.T) {
	illRepo := &MockIllRepo{}
	peer := ill_db.Peer{
		CustomData: map[string]any{
			"ncip": map[string]any{
				"some_key": "some_value",
			},
		},
	}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{peer}, "", nil)
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := pgtype.Text{String: "TEST", Valid: true}
	LmsAdapter, err := creator.GetAdapter(ctx, symbol)
	assert.NoError(t, err)
	assert.IsType(t, &LmsAdapterNcip{}, LmsAdapter)
}

type MockIllRepo struct {
	mock.Mock
	ill_db.PgIllRepo
}

func (r *MockIllRepo) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryLookupAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	args := r.Called(symbols)
	return args.Get(0).([]ill_db.Peer), args.String(1), args.Error(2)
}
