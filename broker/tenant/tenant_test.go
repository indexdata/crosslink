package tenant

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

func mustResolve(t *testing.T, tenantResolver *TenantResolver, ctx common.ExtendedContext, r *http.Request, symbol *string) Tenant {
	tenant, err := tenantResolver.Resolve(ctx, r, symbol)
	assert.NoError(t, err)
	return tenant
}

func TestTenantNoSymbol(t *testing.T) {
	tenantResolver := NewResolver()
	assert.False(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/test"}
	httpRequest := &http.Request{Header: header, URL: turl}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	outputSymbol, err := tenant.GetRequestSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "", outputSymbol)

	ownedSymbols, err := tenant.GetOwnedSymbols()
	assert.NoError(t, err)
	assert.Nil(t, ownedSymbols)

	isOwner, err := tenant.IsOwnerOf("LIB")
	assert.NoError(t, err)
	assert.True(t, isOwner)
}

func TestTenantWithSymbol(t *testing.T) {
	tenantResolver := NewResolver()
	assert.False(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/test"}
	httpRequest := &http.Request{Header: header, URL: turl}

	symbol := "LIB"
	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, &symbol)
	outputSymbol, err := tenant.GetRequestSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "LIB", outputSymbol)

	ownedSymbols, err := tenant.GetOwnedSymbols()
	assert.Error(t, err)
	assert.Nil(t, ownedSymbols)

	isOwner, err := tenant.IsOwnerOf("LIB")
	assert.NoError(t, err)
	assert.True(t, isOwner)
}

func TestTenantNoMapping(t *testing.T) {
	tenantResolver := NewResolver()
	assert.False(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}

	_, err := tenantResolver.Resolve(ctx, httpRequest, nil)
	assert.Error(t, err)
	assert.Equal(t, "tenant mapping must be specified", err.Error())
}

func TestTenantMissingTenant(t *testing.T) {
	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}")
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}

	_, err := tenantResolver.Resolve(ctx, httpRequest, nil)
	assert.Error(t, err)
	assert.Equal(t, "header X-Okapi-Tenant must be specified", err.Error())
}

func TestTenantMapOK(t *testing.T) {
	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}")
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	outputSymbol, err := tenant.GetRequestSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-TENANT1", outputSymbol)

	ownedSymbols, err := tenant.GetOwnedSymbols()
	assert.Error(t, err)
	assert.Nil(t, ownedSymbols)

	isOwner, err := tenant.IsOwnerOf("ISIL:DK-TENANT1")
	assert.NoError(t, err)
	assert.True(t, isOwner)

	assert.Equal(t, "", tenant.GetUser())
}

func TestTenantGetUserFromOkapiHeader(t *testing.T) {
	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}")
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	header.Set("X-Okapi-User-Id", "okapi-user")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	assert.Equal(t, "okapi-user", tenant.GetUser())
}

func TestTenantGetRemoteHostFromXForwardedFor(t *testing.T) {
	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}")
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.5")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl, RemoteAddr: "10.10.10.10:34343"}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	assert.Equal(t, "198.51.100.10", tenant.GetRemoteHost())
}

func TestTenantGetRemoteHostFromRemoteAddr(t *testing.T) {
	tenantResolver := NewResolver()
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/patron_requests"}
	httpRequest := &http.Request{Header: header, URL: turl, RemoteAddr: "10.10.10.10:34343"}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	assert.Equal(t, "10.10.10.10", tenant.GetRemoteHost())
}

func TestTenantGetRemoteHostEmpty(t *testing.T) {
	tenantResolver := NewResolver()
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/patron_requests"}
	httpRequest := &http.Request{Header: header, URL: turl}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	assert.Equal(t, "", tenant.GetRemoteHost())
}

func TestTenantGetUserFallbackForNonOkapi(t *testing.T) {
	tenantResolver := NewResolver()
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/patron_requests"}
	httpRequest := &http.Request{Header: header, URL: turl, RemoteAddr: "10.10.10.10:34343"}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	assert.Equal(t, "unknown@10.10.10.10", tenant.GetUser())
}

func TestTenantGetUserFallbackForNonOkapiWithoutHost(t *testing.T) {
	tenantResolver := NewResolver()
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/patron_requests"}
	httpRequest := &http.Request{Header: header, URL: turl}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	assert.Equal(t, "unknown@unknown", tenant.GetUser())
}

func TestTenantGetUserFallbackForNonOkapiWithSymbol(t *testing.T) {
	tenantResolver := NewResolver()
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	turl := &url.URL{Path: "/patron_requests"}
	httpRequest := &http.Request{Header: header, URL: turl, RemoteAddr: "10.10.10.10:34343"}
	symbol := "ISIL:DK-LIB"

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, &symbol)
	assert.Equal(t, "unknown@10.10.10.10", tenant.GetUser())
}

func TestTenantGetUserFromXForwardedUserForNonOkapi(t *testing.T) {
	tenantResolver := NewResolver()
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Forwarded-User", "remote-user")
	turl := &url.URL{Path: "/patron_requests"}
	httpRequest := &http.Request{Header: header, URL: turl, RemoteAddr: "10.10.10.10:34343"}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	assert.Equal(t, "remote-user@10.10.10.10", tenant.GetUser())
}

func TestTenantRepo1(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", nil)

	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}

	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, nil)
	outputSymbol, err := tenant.GetRequestSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-TENANT1", outputSymbol)

	ownedSymbols, err := tenant.GetOwnedSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1"}, ownedSymbols)

	isOwner, err := tenant.IsOwnerOf("ISIL:DK-TENANT1")
	assert.NoError(t, err)
	assert.True(t, isOwner)
}

func TestTenantSymIdentical(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", nil)

	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}
	symbol := "ISIL:DK-TENANT1"
	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, &symbol)
	outputSymbol, err := tenant.GetRequestSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-TENANT1", outputSymbol)

	ownedSymbols, err := tenant.GetOwnedSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1"}, ownedSymbols)

	isOwner, err := tenant.IsOwnerOf("ISIL:DK-TENANT1")
	assert.NoError(t, err)
	assert.True(t, isOwner)
}

func TestTenantNoBranchMatch(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", nil)

	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}
	symbol := "LIB"
	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, &symbol)
	_, err := tenant.GetRequestSymbol()
	assert.Error(t, err)
	assert.Equal(t, "symbol is not owned by tenant", err.Error())

	ownedSymbols, err := tenant.GetOwnedSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1"}, ownedSymbols)

	isOwner, err := tenant.IsOwnerOf("LIB")
	assert.NoError(t, err)
	assert.False(t, isOwner)
}

func TestTenantBranchMatch(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{{ID: "ISIL:DK-TENANT1"}}, "", nil)
	mockIllRepo.On("GetBranchSymbolsByPeerId", mock.Anything, mock.Anything).Return([]ill_db.BranchSymbol{{SymbolValue: "ISIL:DK-DIKU"}, {SymbolValue: "ISIL:DK-LIB"}}, nil)

	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}
	symbol := "ISIL:DK-LIB"
	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, &symbol)
	outputSymbol, err := tenant.GetRequestSymbol()
	assert.NoError(t, err)
	assert.Equal(t, "ISIL:DK-LIB", outputSymbol)

	ownedSymbols, err := tenant.GetOwnedSymbols()
	assert.NoError(t, err)
	assert.Equal(t, []string{"ISIL:DK-TENANT1", "ISIL:DK-DIKU", "ISIL:DK-LIB"}, ownedSymbols)

	isOwner, err := tenant.IsOwnerOf("ISIL:DK-LIB")
	assert.NoError(t, err)
	assert.True(t, isOwner)
}

func TestTenantRepoError1(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{}, "", assert.AnError)

	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}
	symbol := "ISIL:DK-LIB"
	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, &symbol)
	_, err := tenant.GetRequestSymbol()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())

	_, err = tenant.GetOwnedSymbols()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())
}

func TestTenantRepoError2(t *testing.T) {
	mockIllRepo := new(MockIllRepo)
	mockIllRepo.On("GetCachedPeersBySymbols", mock.Anything, mock.Anything, mock.Anything).Return([]ill_db.Peer{{ID: "ISIL:DK-TENANT1"}}, "", nil)
	mockIllRepo.On("GetBranchSymbolsByPeerId", mock.Anything, mock.Anything).Return([]ill_db.BranchSymbol{}, assert.AnError)

	tenantResolver := NewResolver().WithTenantToSymbol("ISIL:DK-{tenant}").WithIllRepo(mockIllRepo)
	assert.True(t, tenantResolver.HasTenantMapping())

	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	header := http.Header{}
	header.Set("X-Okapi-Tenant", "tenant1")
	turl := &url.URL{Path: "/broker/"}
	httpRequest := &http.Request{Header: header, URL: turl}
	symbol := "ISIL:DK-LIB"
	tenant := mustResolve(t, tenantResolver, ctx, httpRequest, &symbol)
	_, err := tenant.GetRequestSymbol()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())

	_, err = tenant.GetOwnedSymbols()
	assert.Error(t, err)
	assert.Equal(t, "assert.AnError general error for testing", err.Error())
}
