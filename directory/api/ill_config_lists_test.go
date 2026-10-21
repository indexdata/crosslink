package api

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
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
			assertIllCodeArrayContract(t, schema)
			if name != "availableCopyrightTypes" {
				imported := spec.Components.Schemas["ImportIllConfig"].Value
				require.NotContains(t, imported.Required, name)
				assertIllCodeArrayContract(t, imported.Properties[name].Value)
			}
		}
		require.NoError(t, cfg.VisitJSON(map[string]any{}))
		require.NotContains(t, spec.Components.Schemas["ImportIllConfig"].Value.Properties, "availableCopyrightTypes")
	}
}

func assertIllCodeArrayContract(t *testing.T, schema *openapi3.Schema) {
	t.Helper()
	item := schema.Items.Value
	require.ElementsMatch(t, []string{"code", "defaultDesc"}, item.Required)
	require.Len(t, item.Properties, 2)
	require.True(t, item.Properties["code"].Value.Type.Is("string"))
	require.True(t, item.Properties["defaultDesc"].Value.Type.Is("string"))
	require.NoError(t, schema.VisitJSON([]any{}))
	require.NoError(t, schema.VisitJSON([]any{map[string]any{"code": "Legacy-Custom", "defaultDesc": "Legacy description"}}))
	for _, invalid := range []any{nil, "code", []any{nil}, []any{map[string]any{"code": "A"}}, []any{map[string]any{"defaultDesc": "A"}}, []any{map[string]any{"code": 1, "defaultDesc": "A"}}, []any{map[string]any{"code": "A", "defaultDesc": "B", "extra": true}}} {
		require.Error(t, schema.VisitJSON(invalid))
	}
}

func TestIllConfigListsMapping(t *testing.T) {
	original := []byte(`[{"code":"Legacy","defaultDesc":"Original"}]`)
	for _, tc := range []struct {
		name       string
		value      string
		wantCreate string
		wantPatch  string
	}{
		{name: "omitted", wantPatch: string(original)},
		{name: "empty", value: `[]`, wantCreate: `[]`, wantPatch: `[]`},
		{name: "custom values", value: `[{"code":"Custom-B","defaultDesc":"Second"},{"code":"Custom-A","defaultDesc":"First"}]`,
			wantCreate: `[{"code":"Custom-B","defaultDesc":"Second"},{"code":"Custom-A","defaultDesc":"First"}]`,
			wantPatch:  `[{"code":"Custom-B","defaultDesc":"Second"},{"code":"Custom-A","defaultDesc":"First"}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := map[string]json.RawMessage{}
			if tc.value != "" {
				for _, field := range []string{"availableCopyrightTypes", "unfilledReasons", "cancellationReasons", "loanConditions"} {
					body[field] = json.RawMessage(tc.value)
				}
			}
			raw, err := json.Marshal(body)
			require.NoError(t, err)
			var cfg IllConfig
			require.NoError(t, json.Unmarshal(raw, &cfg))
			created, err := illConfigToDBParams(uuid.New(), cfg)
			require.NoError(t, err)
			patched, err := illConfigPatchToDBParams(uuid.New(), cfg, db.IllConfig{
				AvailableCopyrightTypes: original, UnfilledReasons: original, CancellationReasons: original, LoanConditions: original,
			})
			require.NoError(t, err)
			for _, value := range [][]byte{created.AvailableCopyrightTypes, created.UnfilledReasons, created.CancellationReasons, created.LoanConditions} {
				if tc.wantCreate == "" {
					require.Nil(t, value)
				} else {
					require.JSONEq(t, tc.wantCreate, string(value))
				}
			}
			for _, value := range [][]byte{patched.AvailableCopyrightTypes, patched.UnfilledReasons, patched.CancellationReasons, patched.LoanConditions} {
				require.JSONEq(t, tc.wantPatch, string(value))
			}
		})
	}
}
