package profiles

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinDefinitions(t *testing.T) {
	require.Len(t, builtins, 5)
	for _, name := range []string{"Generic", "Alma", "Sierra", "Koha", "FOLIO"} {
		require.Contains(t, builtins, name)
	}
	before := asObject(builtins)
	_, err := Resolve(entry(t, `{"lmsConfig":{"vendor":"Koha","ncipNamespaceEnabled":true},"catalogConfig":{"holdingsFormat":{"marc":{"mainField":"999","availability":[]}}}}`))
	require.NoError(t, err)
	require.Equal(t, before, asObject(builtins), "directory overrides must not change embedded defaults")
}

func TestDecodeBuiltinRejectsInvalidConfiguration(t *testing.T) {
	for _, data := range []string{
		"illConfig:\n  iso18626Vendor: ReShare\n",
		"lmsConfig:\n  misspelledSetting: true\n",
		"catalogConfig:\n  holdingsFormat:\n    opac:\n      includeTemporaryLocation: yesPlease\n",
		"catalogConfig:\n  holdingsFormat:\n    marc:\n      mainField: 952\n",
		"lmsConfig: {}\nlmsConfig: {}\n",
	} {
		_, err := decodeBuiltin([]byte(data))
		require.Error(t, err, data)
	}
}
