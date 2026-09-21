package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultLoanPeriodPatch(t *testing.T) {
	original := int32(21)
	for _, tc := range []struct {
		body string
		want *int32
	}{
		{`{}`, &original},
		{`{"defaultLoanPeriod":null}`, nil},
		{`{"defaultLoanPeriod":30}`, int32Pointer(30)},
	} {
		var config IllConfig
		require.NoError(t, json.Unmarshal([]byte(tc.body), &config))
		result := illConfigPatchToDBParams(uuid.New(), config, db.IllConfig{DefaultLoanPeriod: &original})
		assert.Equal(t, tc.want, result.DefaultLoanPeriod)
	}
	result := illConfigToDBParams(uuid.New(), IllConfig{})
	assert.Nil(t, result.DefaultLoanPeriod, "no implicit default")
	var config IllConfig
	assert.Error(t, json.Unmarshal([]byte(`{"defaultLoanPeriod":1.5}`), &config))
}

func TestMaxRequestsPerPatronPatch(t *testing.T) {
	original := int32(12)
	for _, tc := range []struct {
		body string
		want *int32
	}{
		{`{}`, &original},
		{`{"maxRequestsPerPatron":null}`, nil},
		{`{"maxRequestsPerPatron":0}`, int32Pointer(0)},
		{`{"maxRequestsPerPatron":25}`, int32Pointer(25)},
	} {
		var config IllConfig
		require.NoError(t, json.Unmarshal([]byte(tc.body), &config))
		result := illConfigPatchToDBParams(uuid.New(), config, db.IllConfig{MaxRequestsPerPatron: &original})
		assert.Equal(t, tc.want, result.MaxRequestsPerPatron)
	}

	result := illConfigToDBParams(uuid.New(), IllConfig{})
	assert.Nil(t, result.MaxRequestsPerPatron, "no implicit default")
}

func int32Pointer(value int32) *int32 { return &value }
