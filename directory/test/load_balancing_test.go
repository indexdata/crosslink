package test

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
)

func TestLoadBalancingPolicyPersistenceAndValidation(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	path := "/entries/by-id/00000000-0000-0000-0000-000000000002"
	for _, tc := range []struct {
		body   string
		status int
		want   any
	}{
		{`{"illConfig":{"loadBalancingPolicy":"proportional"}}`, http.StatusNoContent, "proportional"},
		{`{"illConfig":{"useOfferedCosts":true}}`, http.StatusNoContent, "proportional"},
		{`{"illConfig":{"loadBalancingPolicy":"invalid"}}`, http.StatusBadRequest, "proportional"},
		{`{"illConfig":{"loadBalancingPolicy":""}}`, http.StatusBadRequest, "proportional"},
		{`{"illConfig":{"loadBalancingPolicy":123}}`, http.StatusBadRequest, "proportional"},
		{`{"illConfig":{"loadBalancingPolicy":"deficit"}}`, http.StatusNoContent, "deficit"},
		{`{"illConfig":{"loadBalancingPolicy":null}}`, http.StatusNoContent, nil},
	} {
		response, body := jsonReq(t, http.MethodPatch, path, tc.body, headers)
		require.Equal(t, tc.status, response.StatusCode, body)
		response, body = jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &entry))
		config := entry["illConfig"].(map[string]any)
		assert.Equal(t, tc.want, config["loadBalancingPolicy"])
		if tc.want == nil {
			_, present := config["loadBalancingPolicy"]
			assert.False(t, present)
		}
	}
	_, err := dbpool.Exec(context.Background(), `UPDATE ill_configs SET load_balancing_policy='invalid' WHERE entry='00000000-0000-0000-0000-000000000002'`)
	require.Error(t, err, "database constraint rejects invalid policies")
}
