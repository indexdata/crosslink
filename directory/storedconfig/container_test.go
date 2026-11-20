package storedconfig

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMergeHoldingsParsers checks that selecting a parser repairs a legacy
// two-parser configuration. An omitted or empty selection leaves it invalid.
func TestMergeHoldingsParsers(t *testing.T) {
	stored := json.RawMessage(`{"holdingsFormat":{"marc":{"mainField":"999"},"opac":{}}}`)
	for _, tc := range []struct {
		name, patch, want string
		valid             bool
	}{
		{"omitted parsers", `{}`, string(stored), false},
		{"empty parsers", `{"holdingsFormat":{}}`, string(stored), false},
		{"select a stored parser", `{"holdingsFormat":{"marc":{}}}`, `{"holdingsFormat":{"marc":{"mainField":"999"}}}`, true},
		{"select another parser", `{"holdingsFormat":{"reservoir":{}}}`, `{"holdingsFormat":{"reservoir":{}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			merged, err := Merge(CatalogConfig, stored, json.RawMessage(tc.patch))
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(merged))
			if tc.valid {
				require.NoError(t, Check(CatalogConfig, merged))
			} else {
				require.ErrorContains(t, Check(CatalogConfig, merged), "at most one")
			}
		})
	}
}
