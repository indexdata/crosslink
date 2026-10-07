package profiles

import (
	"encoding/json"
	"testing"

	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/stretchr/testify/require"
)

func specification(t *testing.T) map[string]any {
	t.Helper()
	data, err := dirapi.GetSpecJSON()
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(data, &document))
	return document
}

func TestNewResolverRejectsInvalidSpecification(t *testing.T) {
	for _, document := range []string{"broken", "null", "{}", `{"x-host-profiles":null}`, `{"x-host-profiles":[]}`, `{"x-host-profiles":{}}`} {
		_, err := NewResolver([]byte(document))
		require.Error(t, err, document)
	}
	for _, profile := range []string{"Generic", "Alma", "FOLIO", "Koha", "Sierra"} {
		t.Run("missing "+profile, func(t *testing.T) {
			document := specification(t)
			delete(document["x-host-profiles"].(map[string]any), profile)
			data, err := json.Marshal(document)
			require.NoError(t, err)
			_, err = NewResolver(data)
			require.ErrorContains(t, err, "missing required profile "+profile)
		})
	}
	for _, profile := range []string{
		`null`, `{}`, `{"lmsConfig":null,"catalogConfig":{}}`,
		`{"lmsConfig":{},"catalogConfig":null}`,
		`{"lmsConfig":{},"catalogConfig":{},"illConfig":{}}`,
		`{"lmsConfig":{"misspelledSetting":true},"catalogConfig":{}}`,
		`{"lmsConfig":{},"catalogConfig":{"holdingsFormat":{"opac":{"includeTemporaryLocation":"yesPlease"}}}}`,
		`{"lmsConfig":{},"catalogConfig":{"holdingsFormat":{"marc":{"mainField":952}}}}`,
		`{"lmsConfig":{},"catalogConfig":{"holdingsFormat":{"opac":{"availabilityRule":"invalid"}}}}`,
	} {
		document := specification(t)
		var value any
		require.NoError(t, json.Unmarshal([]byte(profile), &value))
		document["x-host-profiles"].(map[string]any)["Alma"] = value
		data, err := json.Marshal(document)
		require.NoError(t, err)
		_, err = NewResolver(data)
		require.ErrorContains(t, err, "Alma", profile)
	}
}

func TestResolverUsesDirectorySnapshot(t *testing.T) {
	document := specification(t)
	definitions := document["x-host-profiles"].(map[string]any)
	definitions["Sierra"].(map[string]any)["lmsConfig"].(map[string]any)["requestItemRequestType"] = "Page"
	data, err := json.Marshal(document)
	require.NoError(t, err)
	resolver, err := NewResolver(data)
	require.NoError(t, err)
	clear(data) // The resolver must not retain the caller's byte buffer.
	raw := entry(t, `{"lmsConfig":{"vendor":"Sierra"},"catalogConfig":{"profile":"Koha"}}`)
	effective, err := resolver.Resolve(raw)
	require.NoError(t, err)
	require.Equal(t, "Page", *effective.LMS.RequestItemRequestType)
	require.Equal(t, "952", *effective.Catalog.HoldingsFormat.Marc.MainField)
	rules := *effective.Catalog.HoldingsFormat.Marc.Availability
	require.Equal(t, "7", rules[0].SubField)
	require.Equal(t, "0", *rules[0].Value)
	*rules[0].Value = "changed"
	effective, err = resolver.Resolve(raw)
	require.NoError(t, err)
	require.Equal(t, "0", *(*effective.Catalog.HoldingsFormat.Marc.Availability)[0].Value)
}

func TestConcurrentResolutionPreservesSnapshot(t *testing.T) {
	resolver := testResolver(t)
	before := asObject(resolver.definitions)
	raw := entry(t, `{"lmsConfig":{"vendor":"Koha","ncipNamespaceEnabled":true},"catalogConfig":{"holdingsFormat":{"marc":{"mainField":"999","availability":[]}}}}`)
	beforeEntry := asObject(raw)
	t.Cleanup(func() {
		require.Equal(t, before, asObject(resolver.definitions))
		require.Equal(t, beforeEntry, asObject(raw))
	})
	for i := range 16 {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			t.Parallel()
			effective, err := resolver.Resolve(raw)
			require.NoError(t, err)
			require.True(t, *effective.LMS.NcipNamespaceEnabled)
			require.Equal(t, "999", *effective.Catalog.HoldingsFormat.Marc.MainField)
		})
	}
}

func TestUninitializedResolver(t *testing.T) {
	for _, resolver := range []*Resolver{nil, {}} {
		_, err := resolver.Resolve(dirapi.Entry{})
		require.ErrorContains(t, err, "not initialized")
	}
}
