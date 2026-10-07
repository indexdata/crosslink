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
		`{"lmsConfig":{},"catalogConfig":{"holdingsFormat":{"opac":{"misspelledSetting":true}}}}`,
		`{"lmsConfig":{},"catalogConfig":{"queryConfig":{"address":"https://catalog.example/sru"}}}`,
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

func TestNewResolverRejectsEntryFields(t *testing.T) {
	const secret = "private-profile-value"
	for _, tc := range []struct {
		section, field string
		value          any
	}{
		{"lmsConfig", "vendor", "Sierra"},
		{"lmsConfig", "Vendor", "Sierra"},
		{"lmsConfig", "address", "https://lms.example/ncip"},
		{"lmsConfig", "fromAgency", "LIBRARY"},
		{"lmsConfig", "fromAgencyAuthentication", secret},
		{"lmsConfig", "toAgency", "LIBRARY"},
		{"lmsConfig", "itemLocation", "STACKS"},
		{"lmsConfig", "requesterPickupLocation", "DESK"},
		{"lmsConfig", "supplierPickupLocation", "DESK"},
		{"lmsConfig", "requesterPatronPattern", "{requesterSymbol}"},
		{"lmsConfig", "patronProfiles", []any{map[string]any{"code": "staff"}}},
		{"lmsConfig", "NcipNamespaceEnabled", false},
		{"catalogConfig", "profile", "Sierra"},
		{"catalogConfig", "Profile", "Sierra"},
		{"catalogConfig", "sru", map[string]any{"address": "https://catalog.example/sru"}},
		{"catalogConfig", "zoom", map[string]any{"address": "catalog.example:210", "options": map[string]any{"password": secret}}},
		{"catalogConfig", "metadataUpdateMode", "none"},
	} {
		t.Run(tc.section+"."+tc.field, func(t *testing.T) {
			for _, value := range []struct {
				name string
				data any
			}{{"populated", tc.value}, {"empty string", ""}, {"empty object", map[string]any{}}, {"null", nil}} {
				t.Run(value.name, func(t *testing.T) {
					document := specification(t)
					profile := document["x-host-profiles"].(map[string]any)["Generic"].(map[string]any)
					profile[tc.section].(map[string]any)[tc.field] = value.data
					data, err := json.Marshal(document)
					require.NoError(t, err)
					resolver, err := NewResolver(data)
					require.ErrorContains(t, err, "host profile Generic")
					require.ErrorContains(t, err, tc.section+"."+tc.field)
					require.NotContains(t, err.Error(), secret)
					require.Nil(t, resolver)
				})
			}
		})
	}
}

func TestNewResolverAcceptsBehaviorDefaults(t *testing.T) {
	document := specification(t)
	profile := document["x-host-profiles"].(map[string]any)["Generic"].(map[string]any)
	lms := map[string]any{
		"ncipNamespaceEnabled": false, "bibIdNormalization": "none",
		"requestItemRequestType": "Hold", "requestItemRequestScopeType": "Title",
		"requestItemBibIdCode": "SYSNUMBER", "requestItemPickupLocationEnabled": false,
		"lookupUserEnabled": false, "acceptItemEnabled": false, "checkInItemEnabled": false,
		"checkOutItemEnabled": false, "requestItemEnabled": false,
	}
	profile["lmsConfig"] = lms
	profile["catalogConfig"] = map[string]any{
		"queryConfig":    map[string]any{"type": "cql", "title": "title = {term}"},
		"metadataFormat": map[string]any{"marc21": map[string]any{"title": "246$a"}},
		"holdingsFormat": map[string]any{"opac": map[string]any{"includeItemId": false}},
	}
	data, err := json.Marshal(document)
	require.NoError(t, err)
	resolver, err := NewResolver(data)
	require.NoError(t, err)
	effective, err := resolver.Resolve(entry(t, `{"lmsConfig":{},"catalogConfig":{}}`))
	require.NoError(t, err)
	actual := asObject(effective.LMS)
	for key, value := range lms {
		require.Equal(t, value, actual[key], key)
	}
	require.Equal(t, "title = {term}", *effective.Catalog.QueryConfig.Title)
	require.Equal(t, "246$a", *effective.Catalog.MetadataFormat.Marc21.Title)
	require.False(t, *effective.Catalog.HoldingsFormat.Opac.IncludeItemId)
}

func TestProfileRestrictionsDoNotApplyToEntries(t *testing.T) {
	raw := entry(t, `{
		"lmsConfig":{"vendor":"Sierra","address":"https://lms.example/ncip","fromAgency":"LIBRARY",
			"fromAgencyAuthentication":"entry-credential","requesterPickupLocation":"DESK"},
		"catalogConfig":{"profile":"Koha","sru":{"address":"https://catalog.example/sru"}}
	}`)
	effective, err := testResolver(t).Resolve(raw)
	require.NoError(t, err)
	require.Equal(t, "Sierra", effective.LMSVendor)
	require.Equal(t, "Koha", effective.CatalogProfile)
	require.Equal(t, raw.LmsConfig.Address, effective.LMS.Address)
	require.Equal(t, raw.LmsConfig.FromAgency, effective.LMS.FromAgency)
	require.Equal(t, *raw.LmsConfig.FromAgencyAuthentication, *effective.LMS.FromAgencyAuthentication)
	require.Equal(t, *raw.LmsConfig.RequesterPickupLocation, *effective.LMS.RequesterPickupLocation)
	require.Equal(t, raw.CatalogConfig.Sru.Address, effective.Catalog.Sru.Address)
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
