package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sseResponseWriter struct {
	header http.Header
	writes chan string
	status int
}

func newSseResponseWriter() *sseResponseWriter {
	return &sseResponseWriter{
		header: make(http.Header),
		writes: make(chan string, 4),
	}
}

func (w *sseResponseWriter) Header() http.Header {
	return w.header
}

func (w *sseResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *sseResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.writes <- string(data)
	return len(data), nil
}

func (w *sseResponseWriter) FlushError() error {
	return nil
}

func (w *sseResponseWriter) SetWriteDeadline(time.Time) error {
	return nil
}

func newTestSseBroker(heartbeat time.Duration) *SseBroker {
	ctx := common.CreateExtCtxWithLogArgsAndHandler(
		context.Background(),
		nil,
		slog.NewTextHandler(io.Discard, nil),
	)
	return &SseBroker{
		clients:        make(map[string]map[chan string]bool),
		ctx:            ctx,
		tenantResolver: tenant.NewResolver().WithTenantToSymbol("ISIL:{tenant}"),
		heartbeat:      heartbeat,
	}
}

func readSseWrite(t *testing.T, w *sseResponseWriter) string {
	t.Helper()
	select {
	case write := <-w.writes:
		return write
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SSE write")
		return ""
	}
}

func TestSseResponseEstablishesStreamAndSendsHeartbeat(t *testing.T) {
	broker := newTestSseBroker(5 * time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, "/sse/events?side=borrowing&symbol=ISIL:REQ", nil).WithContext(ctx)
	w := newSseResponseWriter()
	done := make(chan struct{})
	go func() {
		broker.ServeHTTP(w, req)
		close(done)
	}()

	assert.Equal(t, "retry: 3000\n\n", readSseWrite(t, w))
	assert.Equal(t, http.StatusOK, w.status)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, ": ping\n\n", readSseWrite(t, w))

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestSseResponseLeavesOkapiCorsHeadersUnset(t *testing.T) {
	broker := newTestSseBroker(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	req := httptest.NewRequest(http.MethodGet, "/broker/sse/events?side=lending&symbol=ISIL:RS1", nil).WithContext(ctx)
	req.Header.Set(tenant.OkapiTenantHeader, "rs1")
	w := newSseResponseWriter()
	done := make(chan struct{})
	go func() {
		broker.ServeHTTP(w, req)
		close(done)
	}()

	assert.Equal(t, "retry: 3000\n\n", readSseWrite(t, w))
	assert.Equal(t, http.StatusOK, w.status)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))

	cancel()
	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func TestRemoveSseClientIsIdempotent(t *testing.T) {
	broker := newTestSseBroker(time.Hour)
	clientChannel := make(chan string)
	broker.clients["borrowingISIL:REQ"] = map[chan string]bool{clientChannel: true}

	broker.mu.Lock()
	removed := broker.removeClientLocked("borrowingISIL:REQ", clientChannel)
	removedAgain := broker.removeClientLocked("borrowingISIL:REQ", clientChannel)
	broker.mu.Unlock()

	assert.True(t, removed)
	assert.False(t, removedAgain)
	_, open := <-clientChannel
	assert.False(t, open)
}
