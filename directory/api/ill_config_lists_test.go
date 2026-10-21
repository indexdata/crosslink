package api

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/domain"
	"github.com/stretchr/testify/require"
)

func TestIllConfigDefaultDataContract(t *testing.T) {
	defaults := map[string]map[string]string{
		"availableCopyrightTypes": {
			"AU-CopyRCatS183ComW":   "Copyright Cat S183 - Commonwealth (Australia)",
			"AU-CopyRCatS183State":  "Copyright Act S183 - State (Australia)",
			"AU-CopyrightActS49":    "Copyright Act S49 (Australia)",
			"AU-CopyrightActS50-1":  "Copyright Act S50 (Australia)",
			"AU-CopyrightActS50-7A": "Copyright Act S50 A (Australia)",
			"AU-CopyrightCleared":   "Copyright Cleared (Australia)",
			"AU-GenBus":             "General Business (Australia)",
			"NZ-CopyrightActS54":    "Copyright Act S54 (New Zealand)",
			"NZ-CopyrightActS55":    "Copyright Act S55 (New Zealand)",
			"Other":                 "Other types of copyright compliance. Use Note in ServiceInfo for details.",
			"UK-CopyRFeePaid":       "Copyright Fee Paid (UK)",
			"UK-FairDealing":        "Fair Dealing (UK)",
			"US-CCG":                "CCG (US)",
			"US-CCL":                "CCL (US code)",
		},
		"loanConditions": {
			"LibraryUseOnly":      "Use in library only",
			"WatchLibraryUseOnly": "Watch over use in library only",
			"NoReproduction":      "No reproduction",
			"SignatureRequired":   "Signature required",
			"SpecCollSupervReq":   "Special collections supervision required",
		},
		"unfilledReasons": {
			"NonCirculating": "Non-circulating (e.g. handbook)", "NotAvailableForILL": "Not available for ILL",
			"NotHeld": "Not held", "NotOnShelf": "Not on shelf", "PolicyProblem": "Policy problem", "PoorCondition": "Poor condition",
		},
		"cancellationReasons": {
			"DuplicateRequest": "Duplicate request", "NotFound": "Not found as cited",
			"LocallyAvailable": "Requested item is locally available", "AccountInvalid": "User account is invalid", "UserRequest": "User requested cancellation",
		},
	}
	for _, spec := range loadImportContracts(t) {
		require.NoError(t, spec.Validate(t.Context()))
		// Other modules generate bindings from this schema without Directory Go types.
		item := spec.Components.Schemas["IllConfigCodeDescription"].Value
		require.NotContains(t, item.Extensions, "x-go-type")
		require.NotContains(t, item.Extensions, "x-go-type-import")
		cfg := spec.Components.Schemas["IllConfig"].Value
		require.NotContains(t, cfg.Properties, "cannotSupplyReasons")
		for name, want := range defaults {
			schema := cfg.Properties[name].Value
			require.True(t, schema.Type.Is("array"))
			require.NotContains(t, cfg.Required, name)
			require.Nil(t, schema.Default)
			raw, err := json.Marshal(schema.Extensions["x-default-data"])
			require.NoError(t, err)
			var actual map[string]string
			require.NoError(t, json.Unmarshal(raw, &actual))
			require.Equal(t, want, actual)
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
	original := domain.IllConfigCodeDescriptions{{Code: "Legacy", DefaultDesc: "Original"}}
	for _, tc := range []struct {
		name       string
		body       string
		wantCreate *domain.IllConfigCodeDescriptions
		wantPatch  *domain.IllConfigCodeDescriptions
	}{
		{name: "omitted", body: `{}`, wantPatch: &original},
		{name: "empty", body: `{"unfilledReasons":[],"cancellationReasons":[],"loanConditions":[]}`, wantCreate: &domain.IllConfigCodeDescriptions{}, wantPatch: &domain.IllConfigCodeDescriptions{}},
		{name: "custom values", body: `{"unfilledReasons":[{"code":"Custom-B","defaultDesc":"Second"},{"code":"Custom-A","defaultDesc":"First"}],"cancellationReasons":[{"code":"Custom-B","defaultDesc":"Second"},{"code":"Custom-A","defaultDesc":"First"}],"loanConditions":[{"code":"Custom-B","defaultDesc":"Second"},{"code":"Custom-A","defaultDesc":"First"}]}`,
			wantCreate: &domain.IllConfigCodeDescriptions{{Code: "Custom-B", DefaultDesc: "Second"}, {Code: "Custom-A", DefaultDesc: "First"}},
			wantPatch:  &domain.IllConfigCodeDescriptions{{Code: "Custom-B", DefaultDesc: "Second"}, {Code: "Custom-A", DefaultDesc: "First"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg IllConfig
			require.NoError(t, json.Unmarshal([]byte(tc.body), &cfg))
			created := illConfigToDBParams(uuid.New(), cfg)
			require.Equal(t, tc.wantCreate, created.UnfilledReasons)
			require.Equal(t, tc.wantCreate, created.CancellationReasons)
			require.Equal(t, tc.wantCreate, created.LoanConditions)
			patched := illConfigPatchToDBParams(uuid.New(), cfg, db.IllConfig{UnfilledReasons: &original, CancellationReasons: &original, LoanConditions: &original})
			require.Equal(t, tc.wantPatch, patched.UnfilledReasons)
			require.Equal(t, tc.wantPatch, patched.CancellationReasons)
			require.Equal(t, tc.wantPatch, patched.LoanConditions)
		})
	}
}
