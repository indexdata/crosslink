package api

import (
	"context"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestSymbolChecker(t *testing.T) {
	tenantResolver := common.NewTenant("")
	symbolChecker := NewSymbolChecker(tenantResolver)

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	_, err := symbolChecker.Check(ctx, false, nil, nil)
	assert.Error(t, err)
	assert.Equal(t, "symbol must be specified", err.Error())

	requestSymbol := ""
	_, err = symbolChecker.Check(ctx, false, nil, &requestSymbol)
	assert.Error(t, err)
	assert.Equal(t, "symbol must be specified", err.Error())

	requestSymbol = "symbol2"
	symbol, err := symbolChecker.Check(ctx, false, nil, &requestSymbol)
	assert.NoError(t, err)
	assert.Equal(t, requestSymbol, symbol)

	_, err = symbolChecker.Check(ctx, true, nil, nil)
	assert.Error(t, err)
	assert.Equal(t, "tenant mapping must be specified", err.Error())

	tenantResolver = common.NewTenant("{tenant}")
	symbolChecker = NewSymbolChecker(tenantResolver)
	_, err = symbolChecker.Check(ctx, true, nil, nil)
	assert.Error(t, err)
	assert.Equal(t, "X-Okapi-Tenant must be specified", err.Error())

	tenant := ""
	_, err = symbolChecker.Check(ctx, true, &tenant, nil)
	assert.Error(t, err)
	assert.Equal(t, "X-Okapi-Tenant must be specified", err.Error())

	tenant = "tenant1"
	symbol, err = symbolChecker.Check(ctx, true, &tenant, nil)
	assert.NoError(t, err)
	assert.Equal(t, strings.ToUpper(tenant), symbol)
}

type MockDirectoryLookupAdapter struct {
	mock.Mock
	adapter.DirectoryLookupAdapter
}

type MockIllRepo struct {
	mock.Mock
	ill_db.IllRepo
}

func (r *MockIllRepo) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	args := r.Called(ctx, symbols, directoryAdapter)
	return args.Get(0).([]ill_db.Peer), args.String(1), args.Error(2)
}

func (r *MockIllRepo) GetBranchSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.BranchSymbol, error) {
	args := r.Called(ctx, peerId)
	return args.Get(0).([]ill_db.BranchSymbol), args.Error(1)
}

func TestSymbolCheckerRepoNoPeer(t *testing.T) {
	tenantResolver := common.NewTenant("{tenant}")

	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", nil)

	symbolChecker := NewSymbolChecker(tenantResolver).WithLookupAdapter(&MockDirectoryLookupAdapter{}).WithRepoCheck(mockIllRepo)

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	requestSymbol := "SYMBOL2"
	_, err := symbolChecker.Check(ctx, false, nil, &requestSymbol)
	assert.Error(t, err)
	assert.Equal(t, "no peers for symbol", err.Error())
}

func TestSymbolCheckerRepoOK(t *testing.T) {
	tenantResolver := common.NewTenant("{tenant}")

	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{{ID: "SYMBOL"}}, "", nil)
	mockIllRepo.On("GetBranchSymbolsByPeerId", mock.Anything, mock.Anything).Return([]ill_db.BranchSymbol{{SymbolValue: "LIB"}}, nil)

	symbolChecker := NewSymbolChecker(tenantResolver).WithLookupAdapter(&MockDirectoryLookupAdapter{}).WithRepoCheck(mockIllRepo)

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	requestSymbol := "SYMBOL"
	symbol, err := symbolChecker.Check(ctx, false, nil, &requestSymbol)
	assert.NoError(t, err)
	assert.Equal(t, requestSymbol, symbol)

	requestSymbol = "LIB"
	tenant := "SYMBOL"
	symbol, err = symbolChecker.Check(ctx, true, &tenant, &requestSymbol)
	assert.NoError(t, err)
	assert.Equal(t, requestSymbol, symbol)
}

func TestSymbolCheckerRepoBranches(t *testing.T) {
	tenantResolver := common.NewTenant("{tenant}")

	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{{ID: "SYMBOL"}}, "", nil)
	mockIllRepo.On("GetBranchSymbolsByPeerId", mock.Anything, mock.Anything).Return([]ill_db.BranchSymbol{}, nil)

	symbolChecker := NewSymbolChecker(tenantResolver).WithLookupAdapter(&MockDirectoryLookupAdapter{}).WithRepoCheck(mockIllRepo)

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	requestSymbol := "SYMBOL"
	symbol, err := symbolChecker.Check(ctx, false, nil, &requestSymbol)
	assert.NoError(t, err)
	assert.Equal(t, requestSymbol, symbol)

	requestSymbol = "LIB"
	tenant := "SYMBOL"
	_, err = symbolChecker.Check(ctx, true, &tenant, &requestSymbol)
	assert.Error(t, err)
	assert.Equal(t, "symbol does not match any branch symbols for tenant", err.Error())
}
