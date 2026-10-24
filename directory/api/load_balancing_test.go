package api

import (
	"encoding/json"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLoadBalancingPolicyPatch(t *testing.T) {
	original := "proportional"
	deficit := "deficit"
	for _, tc := range []struct {
		body string
		want *string
	}{
		{`{}`, &original}, {`{"loadBalancingPolicy":null}`, nil}, {`{"loadBalancingPolicy":"deficit"}`, &deficit},
	} {
		var cfg IllConfig
		require.NoError(t, json.Unmarshal([]byte(tc.body), &cfg))
		result, err := illConfigPatchToDBParams(uuid.New(), cfg, db.IllConfig{LoadBalancingPolicy: &original})
		require.NoError(t, err)
		assert.Equal(t, tc.want, result.LoadBalancingPolicy)
	}
	result, err := illConfigToDBParams(uuid.New(), IllConfig{})
	require.NoError(t, err)
	assert.Nil(t, result.LoadBalancingPolicy)
	var cfg IllConfig
	require.NoError(t, json.Unmarshal([]byte(`{"loadBalancingPolicy":"proportional"}`), &cfg))
	result, err = illConfigToDBParams(uuid.New(), cfg)
	require.NoError(t, err)
	assert.Equal(t, &original, result.LoadBalancingPolicy)
}
