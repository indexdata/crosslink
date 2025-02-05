package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleRequest(t *testing.T) {
	req, _ := http.NewRequest("POST", "/", bytes.NewReader([]byte("hello")))
	req.Header.Add("Content-Type", "application/xml")
	rr := httptest.NewRecorder()
	HandleRequest(rr, req)
	// Check the response
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			status, http.StatusOK)
	}
}

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
