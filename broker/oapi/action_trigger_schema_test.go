package oapi

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelActionTriggerSchema(t *testing.T) {
	schema := loadTestSpec(t).Components.Schemas["ModelAction"].Value
	for _, tc := range []struct {
		name  string
		value map[string]any
		valid bool
	}{
		{name: "omitted", value: map[string]any{"name": "overdue"}, valid: true},
		{name: "manual", value: map[string]any{"name": "overdue", "trigger": "manual"}, valid: true},
		{name: "auto", value: map[string]any{"name": "overdue", "trigger": "auto"}, valid: true},
		{name: "background", value: map[string]any{"name": "overdue", "trigger": "background"}, valid: true},
		{name: "unknown", value: map[string]any{"name": "overdue", "trigger": "other"}},
		{name: "empty", value: map[string]any{"name": "overdue", "trigger": ""}},
		{name: "boolean", value: map[string]any{"name": "overdue", "trigger": true}},
		{name: "number", value: map[string]any{"name": "overdue", "trigger": 0}},
		{name: "null", value: map[string]any{"name": "overdue", "trigger": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := schema.VisitJSON(tc.value)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	require.Equal(t, "manual", schema.Properties["trigger"].Value.Default)
}
