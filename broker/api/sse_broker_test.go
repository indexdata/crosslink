package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sseResponseWriter struct {
	header    http.Header
	writes    chan string
	status    int
	mu        sync.Mutex
	pending   strings.Builder
	flushes   int
	deadlines []time.Time
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
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status == 0 {
		w.status = http.StatusOK
	}
	_, _ = w.pending.Write(data)
	return len(data), nil
}

func (w *sseResponseWriter) FlushError() error {
	w.mu.Lock()
	frame := w.pending.String()
	w.pending.Reset()
	w.flushes++
	w.mu.Unlock()
	w.writes <- frame
	return nil
}

func (w *sseResponseWriter) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func (w *sseResponseWriter) flushCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushes
}

func (w *sseResponseWriter) writeDeadlines() []time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]time.Time(nil), w.deadlines...)
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

func registeredSseClient(t *testing.T, broker *SseBroker, receiver string) chan string {
	t.Helper()
	broker.mu.Lock()
	defer broker.mu.Unlock()
	for client := range broker.clients[receiver] {
		return client
	}
	t.Fatalf("no SSE client registered for %s", receiver)
	return nil
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
	assert.Equal(t, 1, w.flushCount())
	deadlines := w.writeDeadlines()
	require.Len(t, deadlines, 1)
	assert.True(t, deadlines[0].IsZero())
	assert.Equal(t, ": ping\n\n", readSseWrite(t, w))
	assert.Equal(t, 2, w.flushCount())

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

func TestSseResponseWritesAndFlushesDataEvent(t *testing.T) {
	broker := newTestSseBroker(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/sse/events?side=borrowing&symbol=ISIL:REQ", nil).WithContext(ctx)
	w := newSseResponseWriter()
	done := make(chan struct{})
	go func() {
		broker.ServeHTTP(w, req)
		close(done)
	}()

	assert.Equal(t, "retry: 3000\n\n", readSseWrite(t, w))
	client := registeredSseClient(t, broker, "borrowingISIL:REQ")
	client <- `{"event":"message-requester"}`
	assert.Equal(t, "data: {\"event\":\"message-requester\"}\n\n", readSseWrite(t, w))
	assert.Equal(t, 2, w.flushCount())

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

func TestSseHandlerExitsWhenClientIsEvicted(t *testing.T) {
	broker := newTestSseBroker(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/sse/events?side=borrowing&symbol=ISIL:REQ", nil).WithContext(ctx)
	w := newSseResponseWriter()
	done := make(chan struct{})
	go func() {
		broker.ServeHTTP(w, req)
		close(done)
	}()

	assert.Equal(t, "retry: 3000\n\n", readSseWrite(t, w))
	client := registeredSseClient(t, broker, "borrowingISIL:REQ")
	broker.mu.Lock()
	removed := broker.removeClientLocked("borrowingISIL:REQ", client)
	broker.mu.Unlock()
	require.True(t, removed)

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
