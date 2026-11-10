package prapi

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRequestIdPrefixResolver struct {
	prefix string
	calls  []string
}

func (s *stubRequestIdPrefixResolver) RequestIdPrefix(_ common.ExtendedContext, requesterSymbol string) (string, error) {
	s.calls = append(s.calls, requesterSymbol)
	return s.prefix, nil
}

func TestParseAndValidateIllRequestPrefix(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	requester := "ISIL:REQ1"
	newRequest := func(id *string) *proapi.CreatePatronRequest {
		return &proapi.CreatePatronRequest{Id: id, RequesterSymbol: &requester, IllRequest: validIllRequest()}
	}

	t.Run("resolved prefix", func(t *testing.T) {
		handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
		resolver := &stubRequestIdPrefixResolver{prefix: "abc"}
		handler.SetRequestIdPrefixResolver(resolver)
		_, id, err := handler.parseAndValidateIllRequest(ctx, newRequest(nil), time.Now())
		require.NoError(t, err)
		assert.Equal(t, "ABC-1", id)
		assert.Equal(t, []string{requester}, resolver.calls)
	})

	t.Run("no resolver uses symbol", func(t *testing.T) {
		handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
		_, id, err := handler.parseAndValidateIllRequest(ctx, newRequest(nil), time.Now())
		require.NoError(t, err)
		assert.Equal(t, "REQ1-1", id)
	})

	t.Run("explicit id skips resolver", func(t *testing.T) {
		handler := NewPrApiHandler(new(PrRepoError), mockEventBus, mockEventRepo, tenant.NewResolver(), nil, 10)
		resolver := &stubRequestIdPrefixResolver{prefix: "abc"}
		handler.SetRequestIdPrefixResolver(resolver)
		explicit := uuid.NewString()
		_, id, err := handler.parseAndValidateIllRequest(ctx, newRequest(&explicit), time.Now())
		require.NoError(t, err)
		assert.Equal(t, explicit, id)
		assert.Empty(t, resolver.calls)
	})
}
