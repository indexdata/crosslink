package enhancedcontext

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnhancedContext(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originalRequestPtr := GetRequest(r.Context())

		if originalRequestPtr == nil {
			t.Error("Tried to retrieve stored request, but got nil")
		}

		if originalRequestPtr.URL != r.URL {
			t.Errorf("Expected URL %s, but got %s", r.URL, originalRequestPtr.URL)
		}

		w.WriteHeader(http.StatusOK)
	})

	middleware := EnhancedContextMiddleware(dummyHandler)

	req := httptest.NewRequest("GET", "http://notarealurl/notreal", nil)
	recorder := httptest.NewRecorder()

	middleware.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status code 200, got %d", recorder.Code)
	}
}

func TestGetRequestEmptyContext(t *testing.T) {
	req := GetRequest(httptest.NewRequest("GET", "/", nil).Context())
	if req != nil {
		t.Errorf("Expected nil, but got %v", req)
	}
}
