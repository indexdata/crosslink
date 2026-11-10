package test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultLoanPeriodPersistenceAndValidation(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	path := "/entries/by-id/00000000-0000-0000-0000-000000000002"
	for _, tc := range []struct {
		value         string
		status        int
		local, merged any
	}{
		{"21", http.StatusNoContent, float64(21), float64(21)},
		// 0 is the off value: stored locally, omitted from the merged view.
		{"0", http.StatusNoContent, float64(0), nil},
		{"-1", http.StatusBadRequest, float64(0), nil},
		{"1.5", http.StatusBadRequest, float64(0), nil},
		{"null", http.StatusNoContent, nil, nil},
	} {
		response, body := jsonReq(t, http.MethodPatch, path, `{"illConfig":{"defaultLoanPeriod":`+tc.value+`}}`, headers)
		require.Equal(t, tc.status, response.StatusCode, body)
		for query, want := range map[string]any{"?resolve-inherit=false": tc.local, "": tc.merged} {
			response, body = jsonReq(t, http.MethodGet, path+query, "", headers)
			require.Equal(t, http.StatusOK, response.StatusCode, body)
			var entry map[string]any
			require.NoError(t, json.Unmarshal([]byte(body), &entry))
			config, _ := entry["illConfig"].(map[string]any)
			assert.Equal(t, want, config["defaultLoanPeriod"], "value %s, query %q", tc.value, query)
		}
	}
}

func TestMaxRequestsPerPatronPersistenceAndValidation(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	path := "/entries/by-id/00000000-0000-0000-0000-000000000002"
	for _, tc := range []struct {
		value  string
		status int
		want   any
	}{
		{"0", http.StatusNoContent, float64(0)},
		{"25", http.StatusNoContent, float64(25)},
		{"-1", http.StatusBadRequest, float64(25)},
		{"1.5", http.StatusBadRequest, float64(25)},
		{"null", http.StatusNoContent, nil},
	} {
		response, body := jsonReq(t, http.MethodPatch, path, `{"illConfig":{"maxRequestsPerPatron":`+tc.value+`}}`, headers)
		require.Equal(t, tc.status, response.StatusCode, body)
		response, body = jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &entry))
		config := entry["illConfig"].(map[string]any)
		assert.Equal(t, tc.want, config["maxRequestsPerPatron"])
	}
}

func TestMinimumCostPersistenceAndValidation(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	path := "/entries/by-id/00000000-0000-0000-0000-000000000002"
	for _, tc := range []struct {
		value  string
		status int
		want   any
	}{
		{"0", http.StatusNoContent, float64(0)},
		{"2.75", http.StatusNoContent, float64(2.75)},
		{"-1", http.StatusBadRequest, float64(2.75)},
		{"null", http.StatusNoContent, nil},
	} {
		response, body := jsonReq(t, http.MethodPatch, path, `{"illConfig":{"minimumCost":`+tc.value+`}}`, headers)
		require.Equal(t, tc.status, response.StatusCode, body)
		response, body = jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &entry))
		config := entry["illConfig"].(map[string]any)
		assert.Equal(t, tc.want, config["minimumCost"])
	}
}
