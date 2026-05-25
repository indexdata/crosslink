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

func TestOpenAPIRequestValidatorHandlesPatronRequestBasePaths(t *testing.T) {
	validator, err := newOpenAPIRequestValidator()
	assert.NoError(t, err)

	for _, path := range []string{"/patron_requests", "/broker/patron_requests"} {
		t.Run(path, func(t *testing.T) {
			called := false
			handler := validator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				assert.Equal(t, path, r.URL.Path)
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(`{"illRequest":{}}`)))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			assert.True(t, called)
			assert.Equal(t, http.StatusNoContent, rr.Code)
		})
	}
}

func TestOpenAPIRequestValidatorRejectsMissingPatronRequestBody(t *testing.T) {
	validator, err := newOpenAPIRequestValidator()
	assert.NoError(t, err)
	handler := validator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for invalid request")
	}))

	req := httptest.NewRequest(http.MethodPost, "/broker/patron_requests", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"error":"request body has an error: value is required but missing"}`, rr.Body.String())
}

func TestOpenAPIRequestValidatorRejectsMissingIllRequest(t *testing.T) {
	validator, err := newOpenAPIRequestValidator()
	assert.NoError(t, err)
	handler := validator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for invalid request")
	}))

	req := httptest.NewRequest(http.MethodPost, "/broker/patron_requests", bytes.NewReader([]byte(`{"requesterSymbol":"ISIL:REQ"}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), `property \"illRequest\" is missing`)
}

func TestOpenAPIRequestValidatorRejectsNullNonNullableString(t *testing.T) {
	validator, err := newOpenAPIRequestValidator()
	assert.NoError(t, err)
	handler := validator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called for invalid request")
	}))

	req := httptest.NewRequest(http.MethodPost, "/broker/patron_requests", bytes.NewReader([]byte(`{"illRequest":{},"requesterSymbol":null}`)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Contains(t, rr.Body.String(), `Error at \"/requesterSymbol\": Value is not nullable`)
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
