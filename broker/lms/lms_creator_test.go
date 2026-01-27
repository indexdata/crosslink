package lms

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

func TestGetAdapterGetCachedByPeersByPeersFail(t *testing.T) {
	illRepo := &MockIllRepo{}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{}, "", assert.AnError)
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	_, err := creator.GetAdapter(ctx, symbol)
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())
}

func TestGetAdapterNoPeers(t *testing.T) {
	illRepo := &MockIllRepo{}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{}, "", nil)
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	LmsAdapter, err := creator.GetAdapter(ctx, symbol)
	assert.NoError(t, err)
	assert.IsType(t, &LmsAdapterManual{}, LmsAdapter)
}

func strPtr(s string) *string {
	return &s
}

func TestGetAdapterNcipOK(t *testing.T) {
	illRepo := &MockIllRepo{}
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			LmsConfig: &directory.LmsConfig{
				Address:                  "http://ncip.example.com",
				FromAgency:               "AGENCY1",
				ToAgency:                 strPtr("AGENCY2"),
				FromAgencyAuthentication: strPtr("auth"),
			},
		},
	}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{peer}, "", nil)
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	LmsAdapter, err := creator.GetAdapter(ctx, symbol)
	assert.NoError(t, err)
	assert.IsType(t, &LmsAdapterNcip{}, LmsAdapter)
}

func TestGetAdapterNcipFail(t *testing.T) {
	illRepo := &MockIllRepo{}
	peer := ill_db.Peer{
		CustomData: directory.Entry{
			LmsConfig: &directory.LmsConfig{},
		},
	}
	illRepo.On("GetCachedPeersBySymbols", mock.Anything).Return([]ill_db.Peer{peer}, "", nil)
	creator := NewLmsCreator(illRepo, nil)
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	symbol := "TEST"
	_, err := creator.GetAdapter(ctx, symbol)
	assert.Error(t, err)
	assert.Equal(t, "missing NCIP address in LMS configuration", err.Error())
}

type MockIllRepo struct {
	mock.Mock
	ill_db.PgIllRepo
}

func (r *MockIllRepo) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryLookupAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	args := r.Called(symbols)
	return args.Get(0).([]ill_db.Peer), args.String(1), args.Error(2)
}
