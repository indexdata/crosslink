package profiles

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestBuiltinDefinitions(t *testing.T) {
	require.Len(t, builtins, 5)
	for _, name := range []string{"Generic", "Alma", "Sierra", "Koha", "FOLIO"} {
		require.Contains(t, builtins, name)
	}
	before := asObject(builtins)
	_, err := Resolve(entry(t, `{"lmsConfig":{"vendor":"Koha","ncipNamespaceEnabled":true},"catalogConfig":{"holdingsFormat":{"marc":{"mainField":"999","availability":[]}}}}`))
	require.NoError(t, err)
	require.Equal(t, before, asObject(builtins), "directory overrides must not change generated defaults")
}

func TestBuiltinDefinitionsMatchDirectorySchema(t *testing.T) {
	data, err := os.ReadFile("../../directory/api.yaml")
	require.NoError(t, err)
	var schema struct {
		Profiles map[string]any `yaml:"x-host-profiles"`
	}
	require.NoError(t, yaml.Unmarshal(data, &schema))
	require.Equal(t, asObject(schema.Profiles), asObject(builtins))
}
