package utils

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitForServiceUpReservedListenerTimesOut(t *testing.T) {
	listener, err := ListenLocal()
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	// TCP connections can complete, but no HTTP server will answer the request.
	done := make(chan any, 1)
	go func() {
		defer func() { done <- recover() }()
		WaitForServiceUp(ListenerPort(listener))
	}()

	select {
	case result := <-done:
		assert.Equal(t, "failed to start broker", result)
	case <-time.After(4 * time.Second):
		t.Fatal("readiness check did not enforce its two-second deadline")
	}
}

func TestWaitForServiceUpHealthyServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/healthz", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	assert.NotPanics(t, func() { WaitForServiceUp(ListenerPort(server.Listener)) })
}
