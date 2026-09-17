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
		{`{"defaultLoanPeriod":30}`, loanPeriodPointer(30)},
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

func loanPeriodPointer(days int32) *int32 { return &days }
