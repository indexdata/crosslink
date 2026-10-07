// Package profiletest supplies Directory profile snapshots for offline broker tests.
package profiletest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/indexdata/crosslink/broker/profiles"
	dirapi "github.com/indexdata/crosslink/directory/api"
)

// NewResolver returns a fresh resolver using the Directory's bundled specification.
func NewResolver(t testing.TB) *profiles.Resolver {
	t.Helper()
	spec, err := dirapi.GetSpecJSON()
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := profiles.NewResolver(spec)
	if err != nil {
		t.Fatal(err)
	}
	return resolver
}

// NewServer serves the Directory specification for tests that initialize the broker.
func NewServer() (*httptest.Server, error) {
	spec, err := dirapi.GetSpecJSON()
	if err != nil {
		return nil, err
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/directory/openapi.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	})), nil
}
