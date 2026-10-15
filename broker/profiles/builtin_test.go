package profiles

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuiltinDefinitions(t *testing.T) {
	builtins, err := loadBuiltins()
	require.NoError(t, err)
	require.Len(t, builtins, 5)
	for _, name := range []string{"Generic", "Alma", "Sierra", "Koha", "FOLIO"} {
		require.Contains(t, builtins, name)
	}
	before := asObject(builtins)
	_, err = Resolve(entry(t, `{"lmsConfig":{"vendor":"Koha","ncipNamespaceEnabled":true},"catalogConfig":{"holdingsFormat":{"marc":{"mainField":"999","availability":[]}}}}`))
	require.NoError(t, err)
	require.Equal(t, before, asObject(builtins), "directory overrides must not change cached defaults")
}

func TestBuiltinDefinitionsMatchDirectorySchema(t *testing.T) {
	data, err := os.ReadFile("../../directory/api.yaml")
	require.NoError(t, err)
	var schema struct {
		Profiles map[string]any `yaml:"x-host-profiles"`
	}
	require.NoError(t, yaml.Unmarshal(data, &schema))
	builtins, err := loadBuiltins()
	require.NoError(t, err)
	require.Equal(t, asObject(schema.Profiles), asObject(builtins))
}

func TestDecodeBuiltinsRejectsInvalidDefinitions(t *testing.T) {
	cases := []struct {
		name, message string
		mutate        func(object)
	}{
		{"missing", "missing or empty x-host-profiles", func(s object) { delete(s, "x-host-profiles") }},
		{"empty", "missing or empty x-host-profiles", func(s object) { s["x-host-profiles"] = object{} }},
		{"missing supported", "host profile Koha: missing definition", func(s object) { delete(s["x-host-profiles"].(object), "Koha") }},
		{"null supported", "host profile Koha: missing definition", func(s object) { s["x-host-profiles"].(object)["Koha"] = nil }},
		{"unknown name", "host profile Extra: not in HostLmsVendor enum", func(s object) { s["x-host-profiles"].(object)["Extra"] = object{} }},
		{"missing enum", "not in HostLmsVendor enum", func(s object) { delete(s, "components") }},
		{"wrong section", `invalid section "illConfig"`, func(s object) { s["x-host-profiles"].(object)["Alma"].(object)["illConfig"] = object{} }},
		{"unknown nested field", `json: unknown field "unknownSetting"`, func(s object) {
			s["x-host-profiles"].(object)["Alma"].(object)["lmsConfig"] = object{"unknownSetting": true}
		}},
		{"incorrect type", "json: cannot unmarshal", func(s object) {
			s["x-host-profiles"].(object)["Alma"].(object)["lmsConfig"] = object{"ncipNamespaceEnabled": "yesPlease"}
		}},
		{"numeric MARC code", "host profile Koha: validate definition: json: cannot unmarshal", func(s object) {
			s["x-host-profiles"].(object)["Koha"].(object)["catalogConfig"] = object{"holdingsFormat": object{"marc": object{"mainField": 952}}}
		}},
	}
	data, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var schema object
			require.NoError(t, json.Unmarshal(data, &schema))
			tc.mutate(schema)
			invalid, err := json.Marshal(schema)
			require.NoError(t, err)
			profiles, err := decodeBuiltins(invalid)
			require.ErrorContains(t, err, tc.message)
			require.Nil(t, profiles)
		})
	}
	t.Run("malformed JSON", func(t *testing.T) {
		profiles, err := decodeBuiltins([]byte(`{"x-host-profiles":`))
		require.ErrorContains(t, err, "decode directory specification")
		var syntaxError *json.SyntaxError
		require.ErrorAs(t, err, &syntaxError)
		require.Nil(t, profiles)
	})
}

func TestDecodeBuiltinsRejectsSectionCapitalization(t *testing.T) {
	data, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	for _, tc := range []struct{ canonical, invalid string }{
		{"lmsConfig", "LMSConfig"},
		{"lmsConfig", "lmsconfig"},
		{"catalogConfig", "CatalogConfig"},
		{"catalogConfig", "catalogconfig"},
	} {
		for _, alongsideCanonical := range []bool{false, true} {
			name := tc.invalid
			if alongsideCanonical {
				name += " alongside canonical"
			}
			t.Run(name, func(t *testing.T) {
				var schema object
				require.NoError(t, json.Unmarshal(data, &schema))
				alma := schema["x-host-profiles"].(object)["Alma"].(object)
				alma[tc.invalid] = alma[tc.canonical]
				if !alongsideCanonical {
					delete(alma, tc.canonical)
				}
				invalid, err := json.Marshal(schema)
				require.NoError(t, err)
				profiles, err := decodeBuiltins(invalid)
				require.ErrorContains(t, err, "host profile Alma: invalid section")
				require.ErrorContains(t, err, `"`+tc.invalid+`"`)
				require.ErrorContains(t, err, "allowed sections are lmsConfig and catalogConfig")
				require.Nil(t, profiles)
			})
		}
	}
}

func TestDecodeBuiltinsPreservesExplicitValues(t *testing.T) {
	data, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	var schema object
	require.NoError(t, json.Unmarshal(data, &schema))
	schema["x-host-profiles"].(object)["Koha"] = object{
		"lmsConfig": object{"ncipNamespaceEnabled": false, "requestItemRequestType": ""},
		"catalogConfig": object{"holdingsFormat": object{"marc": object{
			"mainField": "952", "availability": []any{},
		}}},
	}
	data, err = json.Marshal(schema)
	require.NoError(t, err)
	profiles, err := decodeBuiltins(data)
	require.NoError(t, err)
	require.Equal(t, object{"ncipNamespaceEnabled": false, "requestItemRequestType": ""}, profiles["Koha"].LMS)
	require.Equal(t, object{"holdingsFormat": object{"marc": object{"mainField": "952", "availability": []any{}}}}, profiles["Koha"].Catalog)
	require.Empty(t, profiles["Alma"].LMS)
}

func TestResolveReturnsCachedBuiltinError(t *testing.T) {
	original := loadBuiltins
	t.Cleanup(func() { loadBuiltins = original })
	cause := errors.New("invalid bundled profiles")
	calls := 0
	loadBuiltins = sync.OnceValues(func() (map[string]builtinProfile, error) {
		calls++
		return nil, cause
	})
	for range 2 {
		effective, err := Resolve(dirapi.Entry{})
		require.ErrorIs(t, err, cause)
		require.ErrorContains(t, err, "resolve host profiles")
		require.Nil(t, effective)
	}
	require.Equal(t, 1, calls)
}

func TestConcurrentProfileResolution(t *testing.T) {
	builtins, err := loadBuiltins()
	require.NoError(t, err)
	before := asObject(builtins)
	// Parallel subtests finish before the parent resumes, so the final comparison
	// checks the shared definitions after all concurrent overrides have resolved.
	t.Run("concurrent overrides", func(t *testing.T) {
		for _, name := range []string{"Generic", "Alma", "Sierra", "Koha", "FOLIO"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				for range 10 {
					effective, err := Resolve(entry(t, `{"lmsConfig":{"vendor":"`+name+`","ncipNamespaceEnabled":false},"catalogConfig":{"holdingsFormat":{"marc":{"mainField":"999","availability":[]}}}}`))
					require.NoError(t, err)
					require.False(t, *effective.LMS.NcipNamespaceEnabled)
					require.Equal(t, "999", *effective.Catalog.HoldingsFormat.Marc.MainField)
					require.Empty(t, *effective.Catalog.HoldingsFormat.Marc.Availability)
				}
			})
		}
	})
	require.Equal(t, before, asObject(builtins))
}
