package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func schema(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("../../../../directory/api.yaml")
	require.NoError(t, err)
	return string(data)
}

func TestGenerate(t *testing.T) {
	input := schema(t)
	first, err := generate([]byte(input))
	require.NoError(t, err)
	second, err := generate([]byte(input))
	require.NoError(t, err)
	require.Equal(t, first, second)
	checked, err := os.ReadFile("../../builtin_gen.go")
	require.NoError(t, err)
	require.Equal(t, checked, first)
	output := string(first)
	for _, value := range []string{`"mainField": "952"`, `"value": "0"`, `"ncipNamespaceEnabled": false`, `"allCirculations": true`, `LMS: object{}`, `"CHECK SHELVES"`} {
		require.Contains(t, output, value)
	}
}

func TestGenerateRejectsInvalidDefinitions(t *testing.T) {
	input := schema(t)
	cases := []struct{ name, input, message string }{
		{"malformed", "x-host-profiles: [", "decode directory schema"},
		{"missing", "openapi: 3.1.0", "missing or empty x-host-profiles"},
		{"empty", "x-host-profiles: {}", "missing or empty x-host-profiles"},
		{"missing supported", strings.Replace(input, "  Koha:\n", "  Other:\n", 1), "host profile Koha: missing definition"},
		{"unknown name", strings.Replace(input, "  Alma:\n", "  Extra: {}\n  Alma:\n", 1), "host profile Extra: not in HostLmsVendor enum"},
		{"wrong section", strings.Replace(input, "    lmsConfig: {}", "    illConfig: {}", 1), `host profile Alma: invalid section "illConfig"; allowed sections are lmsConfig and catalogConfig`},
		{"unknown nested field", strings.Replace(input, "availabilityRule: availableNow", "unknownSetting: availableNow", 1), "host profile Alma: json: unknown field"},
		{"incorrect type", strings.Replace(input, "allCirculations: true", "allCirculations: yesPlease", 1), "host profile Alma: json: cannot unmarshal"},
		{"nonfinite value", strings.Replace(input, "allCirculations: true", "allCirculations: .nan", 1), "host profile Alma: json: unsupported value"},
		{"numeric MARC code", strings.Replace(input, `mainField: "952"`, "mainField: 952", 1), "host profile Koha: json: cannot unmarshal"},
		{"duplicate field", strings.Replace(input, "    lmsConfig: {}", "    lmsConfig: {}\n    lmsConfig: {}", 1), "decode directory schema"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := generate([]byte(tc.input))
			require.ErrorContains(t, err, tc.message)
		})
	}
}

func TestGenerateRejectsSectionCapitalization(t *testing.T) {
	input := schema(t)
	for _, tc := range []struct{ canonical, invalid string }{
		{"lmsConfig", "LMSConfig"},
		{"lmsConfig", "lmsconfig"},
		{"catalogConfig", "CatalogConfig"},
		{"catalogConfig", "catalogconfig"},
	} {
		for _, alongsideCanonical := range []bool{false, true} {
			name := tc.invalid
			replacement := "    " + tc.invalid + ":"
			if alongsideCanonical {
				name += " alongside canonical"
				replacement = "    " + tc.invalid + ": {}\n    " + tc.canonical + ":"
			}
			t.Run(name, func(t *testing.T) {
				invalid := strings.Replace(input, "    "+tc.canonical+":", replacement, 1)
				generated, err := generate([]byte(invalid))
				require.ErrorContains(t, err, "host profile Alma: invalid section")
				require.ErrorContains(t, err, `"`+tc.invalid+`"`)
				require.ErrorContains(t, err, "allowed sections are lmsConfig and catalogConfig")
				require.Nil(t, generated)
			})
		}
	}
}

func TestRunPreservesOutputOnInvalidSection(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "api.yaml"), filepath.Join(dir, "profiles.go")
	require.NoError(t, os.WriteFile(input, []byte(schema(t)), 0600))
	require.NoError(t, run(input, output))
	before, err := os.ReadFile(output)
	require.NoError(t, err)
	invalid := strings.Replace(schema(t), "  Koha:\n    lmsConfig:", "  Koha:\n    LMSConfig:", 1)
	require.NoError(t, os.WriteFile(input, []byte(invalid), 0600))
	require.ErrorContains(t, run(input, output), `host profile Koha: invalid section "LMSConfig"`)
	after, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	input, output := filepath.Join(dir, "api.yaml"), filepath.Join(dir, "profiles.go")
	require.NoError(t, os.WriteFile(input, []byte(schema(t)), 0600))
	require.NoError(t, run(input, output))
	data, err := os.ReadFile(output)
	require.NoError(t, err)
	require.Contains(t, string(data), "var builtins")
	require.ErrorContains(t, run(filepath.Join(dir, "missing"), output), "read directory schema")
	require.ErrorContains(t, run(input, dir), "write profile table")
	require.NoError(t, os.WriteFile(input, []byte("{}"), 0600))
	require.ErrorContains(t, run(input, output), "missing or empty x-host-profiles")
}

func TestLiteralValues(t *testing.T) {
	value, err := literal(map[string]any{"empty": []any{}, "nil": nil, "number": float64(1.5)})
	require.NoError(t, err)
	require.Equal(t, `object{"empty": []any{},"nil": nil,"number": float64(1.5),}`, value)
	for _, unsupported := range []any{1, []any{1}, map[string]any{"bad": 1}} {
		_, err := literal(unsupported)
		require.ErrorContains(t, err, "unexpected JSON value int")
	}
}

func TestCommand(t *testing.T) {
	output := filepath.Join(t.TempDir(), "profiles.go")
	oldArgs, oldFlags := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = oldArgs, oldFlags }()
	os.Args = []string{"genprofiles", "-input", "../../../../directory/api.yaml", "-output", output}
	flag.CommandLine = flag.NewFlagSet("genprofiles", flag.ContinueOnError)
	main()
	generated, err := os.ReadFile(output)
	require.NoError(t, err)
	expected, err := generate([]byte(schema(t)))
	require.NoError(t, err)
	require.Equal(t, expected, generated)
}
