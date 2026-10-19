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
		assert.Equal(t, tc.want, illConfigPatchToDBParams(uuid.New(), cfg, db.IllConfig{LoadBalancingPolicy: &original}).LoadBalancingPolicy)
	}
	assert.Nil(t, illConfigToDBParams(uuid.New(), IllConfig{}).LoadBalancingPolicy)
	var cfg IllConfig
	require.NoError(t, json.Unmarshal([]byte(`{"loadBalancingPolicy":"proportional"}`), &cfg))
	assert.Equal(t, &original, illConfigToDBParams(uuid.New(), cfg).LoadBalancingPolicy)
}
