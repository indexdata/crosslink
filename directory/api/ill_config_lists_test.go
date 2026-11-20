package api

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"
)

func TestIllConfigListsContract(t *testing.T) {
	for _, spec := range loadImportContracts(t) {
		require.NoError(t, spec.Validate(t.Context()))
		// Other modules generate bindings from this schema without Directory Go types.
		item := spec.Components.Schemas["IllConfigCodeDescription"].Value
		require.NotContains(t, item.Extensions, "x-go-type")
		require.NotContains(t, item.Extensions, "x-go-type-import")
		cfg := spec.Components.Schemas["IllConfig"].Value
		require.NotContains(t, cfg.Properties, "cannotSupplyReasons")
		for _, name := range []string{"availableCopyrightTypes", "loanConditions", "unfilledReasons", "cancellationReasons"} {
			schema := cfg.Properties[name].Value
			require.True(t, schema.Type.Is("array"))
			require.NotContains(t, cfg.Required, name)
			require.Nil(t, schema.Default)
			assertIllCodeArrayContract(t, schema, false)
			if name != "availableCopyrightTypes" {
				imported := spec.Components.Schemas["ImportIllConfig"].Value
				require.NotContains(t, imported.Required, name)
				// Import null clears the local list so that an inherited list applies.
				assertIllCodeArrayContract(t, imported.Properties[name].Value, true)
			}
		}
		require.NoError(t, cfg.VisitJSON(map[string]any{}))
		require.NotContains(t, spec.Components.Schemas["ImportIllConfig"].Value.Properties, "availableCopyrightTypes")
	}
}

func assertIllCodeArrayContract(t *testing.T, schema *openapi3.Schema, nullable bool) {
	t.Helper()
	item := schema.Items.Value
	require.ElementsMatch(t, []string{"code", "defaultDesc"}, item.Required)
	require.Len(t, item.Properties, 2)
	require.True(t, item.Properties["code"].Value.Type.Is("string"))
	require.True(t, item.Properties["defaultDesc"].Value.Type.Is("string"))
	require.NoError(t, schema.VisitJSON([]any{}))
	require.NoError(t, schema.VisitJSON([]any{map[string]any{"code": "Legacy-Custom", "defaultDesc": "Legacy description"}}))
	if nullable {
		require.NoError(t, schema.VisitJSON(nil))
	} else {
		require.Error(t, schema.VisitJSON(nil))
	}
	for _, invalid := range []any{"code", []any{nil}, []any{map[string]any{"code": "A"}}, []any{map[string]any{"defaultDesc": "A"}}, []any{map[string]any{"code": 1, "defaultDesc": "A"}}, []any{map[string]any{"code": "A", "defaultDesc": "B", "extra": true}}} {
		require.Error(t, schema.VisitJSON(invalid))
	}
}
