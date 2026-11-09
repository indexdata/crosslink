package dirmock

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestOpenapiIncludesSharedCopyrightDefaults(t *testing.T) {
	mock, err := NewJson(`[]`)
	require.NoError(t, err)
	mux := http.NewServeMux()
	require.NoError(t, mock.HandlerFromMux(mux))

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/directory/openapi.json", nil))
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	spec, err := openapi3.NewLoader().LoadFromData(recorder.Body.Bytes())
	require.NoError(t, err)

	// Compare against the shared source, so stale generated defaults cannot pass.
	source, err := openapi3.NewLoader().LoadFromFile("../../directory/api.yaml")
	require.NoError(t, err)
	sourceConfig := source.Components.Schemas["IllConfig"].Value
	expectedDefaults := sourceConfig.Properties["availableCopyrightTypes"].Value.Extensions["x-default-data"]
	require.NotEmpty(t, expectedDefaults)
	config := spec.Components.Schemas["IllConfig"].Value
	require.Equal(t, expectedDefaults, config.Properties["availableCopyrightTypes"].Value.Extensions["x-default-data"])
}

func TestModuleDescriptorRegistersDirectoryRoutes(t *testing.T) {
	data, err := os.ReadFile("../descriptors/ModuleDescriptor-template.json")
	require.NoError(t, err)
	var descriptor struct {
		Provides []struct {
			Handlers []struct {
				Methods     []string `json:"methods"`
				PathPattern string   `json:"pathPattern"`
			} `json:"handlers"`
		} `json:"provides"`
	}
	require.NoError(t, json.Unmarshal(data, &descriptor))
	routes := make(map[string][]string)
	for _, provided := range descriptor.Provides {
		for _, handler := range provided.Handlers {
			routes[handler.PathPattern] = handler.Methods
		}
	}
	for _, path := range []string{"/directory/entries", "/directory/entries/owned", "/directory/openapi.json"} {
		require.Contains(t, routes[path], http.MethodGet, "Okapi must route %s to the mock", path)
	}
}
