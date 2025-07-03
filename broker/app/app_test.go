package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandleHealthz(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("hello")))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	HandleHealthz(rr, req)
	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

func TestConfigLogger(t *testing.T) {
	ENABLE_JSON_LOG = "true"
	handler := configLog()
	if handler == nil {
		t.Errorf("expected to have handler")
	}
}

func TestMigrationFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := Init(ctx)
	assert.ErrorContains(t, err, "DB migration failed:")
}

func TestBadHoldingsAdapter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	HOLDINGS_ADAPTER = "bad"
	_, err := Init(ctx)
	assert.ErrorContains(t, err, "bad value for HOLDINGS_ADAPTER")
	HOLDINGS_ADAPTER = "mock"
}

func TestBadDirectoryAdapter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	DIRECTORY_ADAPTER = "bad"
	_, err := Init(ctx)
	assert.ErrorContains(t, err, "bad value for DIRECTORY_ADAPTER")
	DIRECTORY_ADAPTER = "mock"
}

func TestBadClientDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	CLIENT_DELAY = "bad"
	_, err := Init(ctx)
	assert.ErrorContains(t, err, "invalid duration \"bad\"")
	CLIENT_DELAY = "0ms"
}
