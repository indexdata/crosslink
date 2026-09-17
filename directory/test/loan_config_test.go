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
		value  string
		status int
	}{
		{"21", http.StatusNoContent}, {"0", http.StatusBadRequest}, {"-1", http.StatusBadRequest}, {"1.5", http.StatusBadRequest}, {"null", http.StatusNoContent},
	} {
		response, body := jsonReq(t, http.MethodPatch, path, `{"illConfig":{"defaultLoanPeriod":`+tc.value+`}}`, headers)
		require.Equal(t, tc.status, response.StatusCode, body)
		response, body = jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &entry))
		config := entry["illConfig"].(map[string]any)
		if tc.value == "null" {
			assert.Nil(t, config["defaultLoanPeriod"])
		} else {
			assert.Equal(t, float64(21), config["defaultLoanPeriod"])
		}
	}
}
