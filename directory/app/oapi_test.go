package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestOapiReturnsCompleteSpecificationWithoutPermissions(t *testing.T) {
	handler := InitHandler(context.Background(), nil)
	jsonData, err := api.GetSpecJSON()
	require.NoError(t, err)
	var expected map[string]any
	require.NoError(t, json.Unmarshal(jsonData, &expected))

	// Compare to the source too, so a stale embedded spec cannot pass this test.
	source, err := openapi3.NewLoader().LoadFromFile("../api.yaml")
	require.NoError(t, err)
	sourceJSON, err := json.Marshal(source)
	require.NoError(t, err)
	require.JSONEq(t, string(sourceJSON), string(jsonData))

	for _, test := range []struct {
		name        string
		query       string
		accept      string
		contentType string
	}{
		{name: "default YAML", contentType: "application/yaml"},
		{name: "explicit YAML", query: "?format=yaml", contentType: "application/yaml"},
		{name: "explicit JSON", query: "?format=json", contentType: "application/json"},
		{name: "Accept does not override default", accept: "application/json", contentType: "application/yaml"},
		{name: "Accept does not override query", query: "?format=json", accept: "application/yaml", contentType: "application/json"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodGet, "/directory/oapi"+test.query, nil)
			request.Header.Set("Accept", test.accept)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			require.Equal(t, test.contentType, response.Header().Get("Content-Type"))

			var actual map[string]any
			if test.contentType == "application/json" {
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &actual))
			} else {
				var document map[string]any
				require.NoError(t, yaml.Unmarshal(response.Body.Bytes(), &document))
				data, err := json.Marshal(document)
				require.NoError(t, err)
				require.NoError(t, json.Unmarshal(data, &actual))
			}
			responseSchema := source.Paths.Find("/oapi").Get.Responses.Value("200").Value.Content[test.contentType].Schema.Value
			require.NoError(t, responseSchema.VisitJSON(actual))
			require.Equal(t, expected, actual)
			paths := actual["paths"].(map[string]any)
			require.Contains(t, paths, "/oapi")
		})
	}
}

func TestOapiRejectsInvalidFormat(t *testing.T) {
	handler := InitHandler(context.Background(), nil)
	for _, format := range []string{"", "xml", "JSON"} {
		t.Run("format="+format, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/directory/oapi?format="+format, nil)
			handler.ServeHTTP(response, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}
