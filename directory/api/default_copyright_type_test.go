package api

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
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

func TestDefaultCopyrightTypeMapping(t *testing.T) {
	original := []byte(`{"code":"Original","defaultDesc":"Original description"}`)
	for _, tc := range []struct {
		name, body, wantCreate, wantPatch string
	}{
		{"omitted", `{}`, "", string(original)},
		{"null", `{"defaultCopyrightType":null}`, "", ""},
		{"object", `{"defaultCopyrightType":{"code":"Custom","defaultDesc":"New"}}`, `{"code":"Custom","defaultDesc":"New"}`, `{"code":"Custom","defaultDesc":"New"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg IllConfig
			require.NoError(t, json.Unmarshal([]byte(tc.body), &cfg))
			created, err := illConfigToDBParams(uuid.New(), cfg)
			require.NoError(t, err)
			patched, err := illConfigPatchToDBParams(uuid.New(), cfg, db.IllConfig{DefaultCopyrightType: original})
			require.NoError(t, err)
			for _, value := range []struct {
				got  []byte
				want string
			}{{created.DefaultCopyrightType, tc.wantCreate}, {patched.DefaultCopyrightType, tc.wantPatch}} {
				if value.want == "" {
					require.Nil(t, value.got)
				} else {
					require.JSONEq(t, value.want, string(value.got))
				}
			}
		})
	}
}
