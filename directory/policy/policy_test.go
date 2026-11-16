package policy_test

import (
	"encoding/json"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/require"

	"github.com/indexdata/crosslink/directory/policy"
)

func loadPolicy(t *testing.T) *policy.Policy {
	t.Helper()
	spec, err := openapi3.NewLoader().LoadFromFile("../api.yaml")
	require.NoError(t, err)
	compiled, err := policy.Compile(spec)
	require.NoError(t, err)
	return compiled
}

func TestCompilePolicyTable(t *testing.T) {
	p := loadPolicy(t)
	require.Equal(t, []string{"catalogConfig", "holdingsPolicy", "illConfig", "lmsConfig"}, p.Containers())
	all := []string{policy.TypeConsortium, policy.TypeInstitution, policy.TypeBranch}
	for _, tc := range []struct {
		container, child string
		root             string
		editable         []string
		unit             string
	}{
		{"catalogConfig", "profile", policy.TypeInstitution, []string{policy.TypeConsortium, policy.TypeInstitution}, ""},
		{"catalogConfig", "sru", policy.TypeInstitution, []string{policy.TypeConsortium, policy.TypeInstitution}, "catalogEndpoint"},
		{"catalogConfig", "zoom", policy.TypeInstitution, []string{policy.TypeConsortium, policy.TypeInstitution}, "catalogEndpoint"},
		{"lmsConfig", "itemLocation", policy.TypeInstitution, []string{policy.TypeInstitution}, ""},
		{"lmsConfig", "address", policy.TypeInstitution, []string{policy.TypeInstitution}, "ncipConnection"},
		{"lmsConfig", "fromAgencyAuthentication", policy.TypeInstitution, []string{policy.TypeInstitution}, "ncipConnection"},
		{"lmsConfig", "requesterPickupLocation", policy.RootNone, []string{policy.TypeInstitution, policy.TypeBranch}, ""},
		{"lmsConfig", "supplierPickupLocation", policy.RootNone, []string{policy.TypeInstitution, policy.TypeBranch}, ""},
		{"illConfig", "maxRequestsPerPatron", policy.TypeConsortium, []string{policy.TypeConsortium, policy.TypeInstitution}, ""},
		{"illConfig", "iso18626Url", policy.TypeInstitution, []string{policy.TypeConsortium, policy.TypeInstitution}, "iso18626Peer"},
		{"illConfig", "iso18626Vendor", policy.TypeInstitution, []string{policy.TypeConsortium, policy.TypeInstitution}, "iso18626Peer"},
		{"illConfig", "isPickupLocation", policy.RootNone, []string{policy.TypeInstitution, policy.TypeBranch}, ""},
		{"illConfig", "loadBalancingPolicy", policy.RootNone, []string{policy.TypeConsortium}, ""},
	} {
		container, ok := p.Container(tc.container)
		require.True(t, ok, tc.container)
		field := container.Child(tc.child)
		require.Equal(t, tc.root, field.Root, "%s.%s root", tc.container, tc.child)
		require.Equal(t, tc.editable, field.EditableOn, "%s.%s editable", tc.container, tc.child)
		require.Equal(t, tc.unit, field.Unit, "%s.%s unit", tc.container, tc.child)
		for _, entryType := range all {
			require.Equal(t, contains(tc.editable, entryType), field.Editable(entryType), "%s.%s on %s", tc.container, tc.child, entryType)
		}
	}
	holdings, _ := p.Container("holdingsPolicy")
	require.True(t, holdings.Atomic)
	require.Equal(t, policy.TypeInstitution, holdings.Root)
	lms, _ := p.Container("lmsConfig")
	require.True(t, lms.Child("fromAgencyAuthentication").Protected)
	require.JSONEq(t, `"default"`, string(lms.Child("ncipVersion").OffValue))
	ill, _ := p.Container("illConfig")
	require.JSONEq(t, `0`, string(ill.Child("defaultLoanPeriod").OffValue))
	require.Nil(t, ill.Child("minimumCost").OffValue)
}

func TestCompileRejectsInvalidAnnotations(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*openapi3.T)
		err  string
	}{
		{"unknown root", func(s *openapi3.T) {
			s.Components.Schemas["EntryProperties"].Value.Properties["lmsConfig"].Value.Extensions["x-inheritance-root"] = "Region"
		}, `unknown x-inheritance-root "Region"`},
		{"branch root", func(s *openapi3.T) {
			s.Components.Schemas["EntryProperties"].Value.Properties["lmsConfig"].Value.Extensions["x-inheritance-root"] = "Branch"
		}, `unknown x-inheritance-root "Branch"`},
		{"unknown editable type", func(s *openapi3.T) {
			s.Components.Schemas["EntryProperties"].Value.Properties["lmsConfig"].Value.Extensions["x-editable-on"] = []any{"Library"}
		}, `unknown entry type "Library"`},
		{"variant annotation", func(s *openapi3.T) {
			s.Components.Schemas["EntryPatch"].Value.Properties["lmsConfig"].Value.Extensions = map[string]any{"x-editable-on": []any{"Branch"}}
		}, "declare policy on EntryProperties only"},
		{"variant field without policy", func(s *openapi3.T) {
			s.Components.Schemas["LmsConfigPatch"].Value.Properties["unknownSetting"] = openapi3.NewStringSchema().NewRef()
		}, "lmsConfig.unknownSetting has no canonical policy"},
		{"inconsistent unit", func(s *openapi3.T) {
			s.Components.Schemas["IllConfig"].Value.Properties["iso18626Url"].Value.Extensions["x-inheritance-root"] = "Consortium"
		}, "merge unit iso18626Peer mixes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec, err := openapi3.NewLoader().LoadFromFile("../api.yaml")
			require.NoError(t, err)
			tc.edit(spec)
			_, err = policy.Compile(spec)
			require.ErrorContains(t, err, tc.err)
		})
	}
}

func level(entryType string, tenant string, configs string) policy.Level {
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal([]byte(configs), &parsed); err != nil {
		panic(err)
	}
	var owner *string
	if tenant != "" {
		owner = &tenant
	}
	return policy.Level{Type: entryType, Tenant: owner, Configs: parsed}
}

func resolve(t *testing.T, p *policy.Policy, chain ...policy.Level) map[string]any {
	t.Helper()
	resolved, err := p.Resolve(chain)
	require.NoError(t, err)
	data, err := json.Marshal(resolved)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

func TestResolveBoundariesAndExceptions(t *testing.T) {
	p := loadPolicy(t)
	consortium := level(policy.TypeConsortium, "C", `{
		"illConfig": {"maxRequestsPerPatron": 10, "iso18626Url": "https://consortium/iso", "loadBalancingPolicy": "deficit", "isPickupLocation": true, "lendersOfLastResort": [{"authority":"ISIL","symbol":"LAST"}]},
		"catalogConfig": {"profile": "Koha"}
	}`)
	institution := level(policy.TypeInstitution, "T", `{
		"illConfig": {"iso18626Url": "https://inst/iso"},
		"lmsConfig": {"vendor": "Horizon", "address": "https://ncip", "fromAgency": "INST", "fromAgencyAuthentication": "secret", "requesterPickupLocation": "MAIN", "itemLocation": "STACKS"},
		"holdingsPolicy": {"locations": []}
	}`)
	branch := level(policy.TypeBranch, "T", `{"lmsConfig": {"requesterPickupLocation": "BR1"}, "name": "kept"}`)

	got := resolve(t, p, institution, consortium)
	require.Equal(t, map[string]any{
		"maxRequestsPerPatron": 10.0,
		"iso18626Url":          "https://inst/iso",
		"lendersOfLastResort":  []any{map[string]any{"authority": "ISIL", "symbol": "LAST"}},
	}, got["illConfig"], "consortium ILL values inherit except ISO peer, load balancing and pickup flag")
	require.NotContains(t, got, "catalogConfig", "consortium catalog configuration is not inherited")

	got = resolve(t, p, branch, institution, consortium)
	require.Equal(t, "kept", got["name"], "unannotated values pass through")
	require.Equal(t, map[string]any{
		"vendor": "Horizon", "address": "https://ncip", "fromAgency": "INST", "fromAgencyAuthentication": "secret",
		"requesterPickupLocation": "BR1", "itemLocation": "STACKS",
	}, got["lmsConfig"])
	require.Equal(t, map[string]any{
		"maxRequestsPerPatron": 10.0,
		"iso18626Url":          "https://inst/iso",
		"lendersOfLastResort":  []any{map[string]any{"authority": "ISIL", "symbol": "LAST"}},
	}, got["illConfig"])
	require.Equal(t, map[string]any{"locations": []any{}}, got["holdingsPolicy"])

	got = resolve(t, p, level(policy.TypeInstitution, "", `{}`))
	require.Empty(t, got, "nothing to inherit leaves fields unset")
}

func TestResolveMergeUnitsAndArrays(t *testing.T) {
	p := loadPolicy(t)
	institution := level(policy.TypeInstitution, "T", `{
		"lmsConfig": {"vendor": "Horizon", "ncipVersion": "1.0", "address": "A", "fromAgency": "INST", "fromAgencyAuthentication": "secret"},
		"catalogConfig": {"sru": {"address": "https://sru"}, "queryConfig": {"type": "cql", "title": "t={term}"}},
		"illConfig": {"lendersOfLastResort": [{"authority":"ISIL","symbol":"LAST"}]}
	}`)
	branch := level(policy.TypeBranch, "T", `{
		"lmsConfig": {"address": "B", "fromAgency": "BR"},
		"catalogConfig": {"zoom": {"address": "z3950"}},
		"illConfig": {"lendersOfLastResort": []}
	}`)
	got := resolve(t, p, branch, institution)
	require.Equal(t, map[string]any{"address": "B", "fromAgency": "BR"}, got["lmsConfig"],
		"a partial legacy unit is taken as a whole and never combined with inherited credentials")
	require.Equal(t, map[string]any{"zoom": map[string]any{"address": "z3950"}, "queryConfig": map[string]any{"type": "cql", "title": "t={term}"}},
		got["catalogConfig"], "the endpoint unit comes from one entry; other fields still inherit")
	require.Equal(t, map[string]any{"lendersOfLastResort": []any{}}, got["illConfig"], "an empty array blocks inheritance")
}

func TestResolveOffValuesAfterResolution(t *testing.T) {
	p := loadPolicy(t)
	consortium := level(policy.TypeConsortium, "", `{"illConfig": {"defaultLoanPeriod": 30, "minimumCost": 5}}`)
	institution := level(policy.TypeInstitution, "T", `{"illConfig": {"defaultLoanPeriod": 0, "minimumCost": 0}, "lmsConfig": {"vendor": "Horizon", "ncipVersion": "default"}}`)
	branch := level(policy.TypeBranch, "T", `{}`)
	require.Equal(t, map[string]any{"defaultLoanPeriod": 30.0, "minimumCost": 5.0}, resolve(t, p, consortium)["illConfig"])
	for _, chain := range [][]policy.Level{{institution, consortium}, {branch, institution, consortium}} {
		got := resolve(t, p, chain...)
		require.Equal(t, map[string]any{"minimumCost": 0.0}, got["illConfig"],
			"a winning off value is omitted and never replaced by a further ancestor's value")
		require.Equal(t, map[string]any{"vendor": "Horizon"}, got["lmsConfig"])
	}
}

func TestResolveProtectedValues(t *testing.T) {
	p := loadPolicy(t)
	institution := level(policy.TypeInstitution, "T", `{"lmsConfig": {"address": "A", "fromAgency": "INST", "fromAgencyAuthentication": "secret"}}`)
	otherTenantBranch := level(policy.TypeBranch, "OTHER", `{}`)
	require.Equal(t, map[string]any{"address": "A", "fromAgency": "INST"}, resolve(t, p, otherTenantBranch, institution)["lmsConfig"],
		"protected values never cross a tenant boundary")

	redacted := institution
	redacted.Redact = true
	require.Equal(t, map[string]any{"address": "A", "fromAgency": "INST", "fromAgencyAuthentication": ""},
		resolve(t, p, level(policy.TypeBranch, "T", `{}`), redacted)["lmsConfig"], "a hidden ancestor value is resolved as an empty string")

	unset := level(policy.TypeInstitution, "T", `{"lmsConfig": {"vendor": "Horizon"}}`)
	unset.Redact = true
	branch := level(policy.TypeBranch, "T", `{"lmsConfig": {"toAgency": "BR"}}`)
	require.Equal(t, map[string]any{"toAgency": "BR"}, resolve(t, p, branch, unset)["lmsConfig"],
		"redaction does not change which entry supplies a merge unit")

	unowned := level(policy.TypeInstitution, "", `{"lmsConfig": {"fromAgencyAuthentication": "secret"}}`)
	require.Equal(t, map[string]any{"fromAgencyAuthentication": "secret"}, resolve(t, p, level(policy.TypeBranch, "", `{}`), unowned)["lmsConfig"])
}

func TestCheckWrite(t *testing.T) {
	p := loadPolicy(t)
	parse := func(document string) map[string]json.RawMessage {
		var parsed map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(document), &parsed))
		return parsed
	}
	stored := parse(`{"lmsConfig": {"address": "LEGACY"}, "holdingsPolicy": {"locations": []}}`)
	for _, tc := range []struct {
		name, entryType, body string
		want                  []string
	}{
		{"branch pickup code", policy.TypeBranch, `{"lmsConfig": {"requesterPickupLocation": "BR1"}, "illConfig": {"isPickupLocation": true}}`, nil},
		{"branch nulls clear restricted values", policy.TypeBranch, `{"lmsConfig": {"address": null, "fromAgency": null}, "illConfig": null, "holdingsPolicy": null}`, nil},
		{"branch resubmits stored value", policy.TypeBranch, `{"lmsConfig": {"address": "LEGACY"}, "holdingsPolicy": {"locations": []}}`, nil},
		{"branch prohibited values", policy.TypeBranch, `{"lmsConfig": {"address": "NEW", "requesterPickupLocation": "BR1"}, "illConfig": {"maxRequestsPerPatron": 5}, "holdingsPolicy": {"locations": [{"code": "X"}]}, "name": "ignored"}`,
			[]string{"holdingsPolicy", "illConfig.maxRequestsPerPatron", "lmsConfig.address"}},
		{"institution load balancing", policy.TypeInstitution, `{"illConfig": {"loadBalancingPolicy": "deficit", "maxRequestsPerPatron": 5}}`, []string{"illConfig.loadBalancingPolicy"}},
		{"consortium lms", policy.TypeConsortium, `{"lmsConfig": {"vendor": "Koha"}, "catalogConfig": {"profile": "Koha"}}`, []string{"lmsConfig.vendor"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := p.CheckWrite(tc.entryType, parse(tc.body), stored)
			require.NoError(t, err)
			var paths []string
			for _, violation := range violations {
				paths = append(paths, violation.Path)
				require.Equal(t, tc.entryType, violation.EntryType)
			}
			require.Equal(t, tc.want, paths)
		})
	}
	violations, _ := p.CheckWrite(policy.TypeBranch, parse(`{"illConfig": {"maxRequestsPerPatron": 5}}`), nil)
	require.Equal(t, "illConfig.maxRequestsPerPatron is not editable on Branch", policy.ViolationMessage(violations))
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
