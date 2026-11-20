package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultCopyrightTypeContract(t *testing.T) {
	for _, spec := range loadImportContracts(t) {
		require.NoError(t, spec.Validate(t.Context()))
		cfg := spec.Components.Schemas["IllConfig"].Value
		field := cfg.Properties["defaultCopyrightType"].Value
		require.Equal(t, "The default copyright type to use for copy requests", field.Description)
		require.Equal(t, cfg.Properties["availableCopyrightTypes"].Value.Extensions["x-default-data"], field.Extensions["x-default-data"])
		require.Nil(t, field.Default)
		for _, name := range []string{"IllConfig", "ImportIllConfig"} {
			config := spec.Components.Schemas[name].Value
			require.NotContains(t, config.Required, "defaultCopyrightType")
			schema := config.Properties["defaultCopyrightType"].Value
			require.Len(t, schema.AnyOf, 2)
			require.Equal(t, "#/components/schemas/IllConfigCodeDescription", schema.AnyOf[0].Ref)
			for _, valid := range []any{nil, map[string]any{"code": "Custom", "defaultDesc": "Custom copyright"}} {
				require.NoError(t, schema.VisitJSON(valid))
			}
			for _, invalid := range []any{"Custom", []any{}, map[string]any{}, map[string]any{"code": "A"}, map[string]any{"defaultDesc": "B"}, map[string]any{"code": 1, "defaultDesc": "B"}, map[string]any{"code": "A", "defaultDesc": "B", "extra": true}} {
				require.Error(t, schema.VisitJSON(invalid))
			}
		}
	}
}
