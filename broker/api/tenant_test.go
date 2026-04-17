package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

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

func TestTenantNoSymbol(t *testing.T) {
	tenantContext := NewTenantContext()
	assert.False(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	url := &url.URL{Path: "/test"}
	httpRequest := &http.Request{Header: header, URL: url}

	tenant := tenantContext.WithRequest(ctx, httpRequest, nil)
	_, err := tenant.GetSymbol()
	assert.Error(t, err)
	assert.Equal(t, "symbol must be specified", err.Error())

	symbols, err := tenant.GetSymbols()
	assert.NoError(t, err)
	assert.Nil(t, symbols)
}

func TestTenantWithSymbol(t *testing.T) {
	tenantContext := NewTenantContext()
	assert.False(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	url := &url.URL{Path: "/test"}
	httpRequest := &http.Request{Header: header, URL: url}

	symbol := "LIB"
	tenant := tenantContext.WithRequest(ctx, httpRequest, &symbol)
	_, err := tenant.GetSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "LIB", symbol)

	symbols, err := tenant.GetSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"LIB"}, symbols)
}

func TestTenantNoMapping(t *testing.T) {
	tenantContext := NewTenantContext()
	assert.False(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}

	tenant := tenantContext.WithRequest(ctx, httpRequest, nil)
	_, err := tenant.GetSymbol()
	assert.Error(t, err)
	assert.Equal(t, "tenant mapping must be specified", err.Error())

	_, err = tenant.GetSymbols()
	assert.Error(t, err)
	assert.Equal(t, "tenant mapping must be specified", err.Error())
}

func TestTenantMissingTenant(t *testing.T) {
	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}")
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}

	tenant := tenantContext.WithRequest(ctx, httpRequest, nil)
	_, err := tenant.GetSymbol()
	assert.Error(t, err)
	assert.Equal(t, "header X-Okapi-Tenant must be specified", err.Error())

	_, err = tenant.GetSymbols()
	assert.Error(t, err)
	assert.Equal(t, "header X-Okapi-Tenant must be specified", err.Error())
}

func TestTenantMapOK(t *testing.T) {
	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}")
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}

	tenant := tenantContext.WithRequest(ctx, httpRequest, nil)
	sym, err := tenant.GetSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-TENANT1", sym)

	symbols, err := tenant.GetSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1"}, symbols)
}

func TestTenantRepo1(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", nil)

	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}

	tenant := tenantContext.WithRequest(ctx, httpRequest, nil)
	sym, err := tenant.GetSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-TENANT1", sym)

	symbols, err := tenant.GetSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1"}, symbols)
}

func TestTenantSymIdentical(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", nil)

	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}
	symbol := "ISIL:DK-TENANT1"
	tenant := tenantContext.WithRequest(ctx, httpRequest, &symbol)
	outputSymbol, err := tenant.GetSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-TENANT1", outputSymbol)

	symbols, err := tenant.GetSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1"}, symbols)
}

func TestTenantNoBranchMatch(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", nil)

	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}
	symbol := "LIB"
	tenant := tenantContext.WithRequest(ctx, httpRequest, &symbol)
	_, err := tenant.GetSymbol()
	assert.Error(t, err)
	assert.Equal(t, "symbol does not match any branch symbols for tenant", err.Error())

	symbols, err := tenant.GetSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1"}, symbols)
}

func TestTenantBranchMatch(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{{ID: "ISIL:DK-TENANT1"}}, "", nil)
	mockIllRepo.On("GetBranchSymbolsByPeerId", mock.Anything, mock.Anything).Return([]ill_db.BranchSymbol{{SymbolValue: "ISIL:DK-DIKU"}, {SymbolValue: "ISIL:DK-LIB"}}, nil)

	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}
	symbol := "ISIL:DK-LIB"
	tenant := tenantContext.WithRequest(ctx, httpRequest, &symbol)
	outputSymbol, err := tenant.GetSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-LIB", outputSymbol)

	symbols, err := tenant.GetSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1", "ISIL:DK-DIKU", "ISIL:DK-LIB"}, symbols)
}

func TestTenantRepoError1(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", assert.AnError)

	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}
	symbol := "ISIL:DK-LIB"
	tenant := tenantContext.WithRequest(ctx, httpRequest, &symbol)
	_, err := tenant.GetSymbol()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())

	_, err = tenant.GetSymbols()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())
}

func TestTenantRepoError2(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{{ID: "ISIL:DK-TENANT1"}}, "", nil)
	mockIllRepo.On("GetBranchSymbolsByPeerId", mock.Anything, mock.Anything).Return([]ill_db.BranchSymbol{}, assert.AnError)

	tenantContext := NewTenantContext().WithTenantSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantContext.isSpecified())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	url := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: url}
	symbol := "ISIL:DK-LIB"
	tenant := tenantContext.WithRequest(ctx, httpRequest, &symbol)
	_, err := tenant.GetSymbol()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())

	_, err = tenant.GetSymbols()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())
}
