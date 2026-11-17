package test

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// Each configuration section, set on create, with every field an institution
// may store.
var fullSections = map[string]string{
	"illConfig": `{
		"isPickupLocation":true,"iso18626Url":"https://iso.example/a","iso18626Vendor":"ReShare",
		"lendersOfLastResort":[{"authority":"TEST","symbol":"ANINST"}],
		"includeRequestingAgencyInfo":true,"includeSupplierInfo":false,"includeReturnInfo":true,
		"includeVendorNote":false,"useOfferedCosts":true,"noteFieldSeparator":" | ",
		"supplierPatronPattern":"P-{requesterSymbol}","requestIdPrefix":"AB","duplicateCheckWindowHours":24,
		"defaultLoanPeriod":21,"maxRequestsPerPatron":12,"minimumCost":1.5,
		"defaultCopyrightType":{"code":"US","defaultDesc":"US copyright"},
		"availableCopyrightTypes":[{"code":"US","defaultDesc":"US copyright"}],
		"unfilledReasons":[{"code":"U1","defaultDesc":"Unfilled"}],
		"loanConditions":[{"code":"L1","defaultDesc":"Library use only"}],
		"cancellationReasons":[{"code":"C1","defaultDesc":"Cancelled"}]
	}`,
	"lmsConfig": `{
		"vendor":"Koha","ncipVersion":"2.02","ncipNamespaceEnabled":true,"bibIdNormalization":"sierra",
		"address":"https://ncip.example","fromAgency":"FROM","fromAgencyAuthentication":"secret","toAgency":"TO",
		"lookupUserEnabled":true,"acceptItemEnabled":false,"checkInItemEnabled":true,"checkOutItemEnabled":false,
		"requestItemEnabled":true,"itemLocation":"STACKS","requestItemRequestType":"Hold",
		"requestItemRequestScopeType":"Item","bibItemScopeName":"Title","requestItemBibIdCode":"SYSNUMBER",
		"requestItemPickupLocationEnabled":true,"requesterPickupLocation":"REQ","supplierPickupLocation":"SUP",
		"requesterPatronPattern":"{patron}","patronProfiles":[{"code":"STAFF","name":"Staff","canCreateRequests":true}]
	}`,
	"catalogConfig": `{
		"profile":"Koha","metadataUpdateMode":"merge",
		"sru":{"address":"https://sru.example","recordSchema":"marcxml"},
		"queryConfig":{"type":"cql","title":"title={term}","identifier":"rec.id={term}","identifierNormalization":"stripLeadingLetters","identifierPrefix":"x"},
		"holdingsFormat":{"marc":{"mainField":"999","itemIdSubfield":"i","availability":[{"subfield":"a","operator":"equals","value":"y"}]}},
		"metadataFormat":{"marc21":{"title":"245$a","author":"100$a"}}
	}`,
	"holdingsPolicy": `{
		"locations":[{"code":"MAIN","name":"Main","supplyPreference":100}],
		"shelvingLocations":[{"code":"STACKS","name":"Stacks","supplyPreference":50}],
		"locationPolicies":[{"locationCode":"MAIN","shelvingLocationCode":"RARE","supplyPreference":-1}],
		"itemLoanPolicies":[{"code":"NORMAL","name":"Normal loan","lendable":true}]
	}`,
}

// createConfigEntry creates an institution under the fixture consortium with
// the given entry properties and returns its path.
func createConfigEntry(t *testing.T, properties string) string {
	t.Helper()
	body := fmt.Sprintf(`{"name":"Config patch","type":"Institution","parent":"%s"%s}`, fixtureConsortium, properties)
	response, data := jsonReq(t, http.MethodPost, "/entries", body, consortialHeaders)
	require.Equal(t, http.StatusCreated, response.StatusCode, data)
	var created struct{ Id string }
	require.NoError(t, json.Unmarshal([]byte(data), &created))
	return "/entries/by-id/" + created.Id
}

func patchEntry(t *testing.T, path, body string) {
	t.Helper()
	response, data := jsonReq(t, http.MethodPatch, path, body, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, data)
}

// localSections returns the configuration sections the entry stores itself.
func localSections(t *testing.T, path string) map[string]any {
	t.Helper()
	entry := getEntryJSON(t, path+"?resolve-inherit=false", systemHeaders)
	sections := map[string]any{}
	for name := range fullSections {
		if value, ok := entry[name]; ok {
			sections[name] = value
		}
	}
	return sections
}

func decodeObject(t *testing.T, data string) map[string]any {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal([]byte(data), &value))
	return value
}

// TestConfigPatchSections checks that PATCH keeps an omitted section, removes
// a null one and stores an empty object as a section with no fields set.
func TestConfigPatchSections(t *testing.T) {
	resetDb()
	all := ""
	for _, name := range slices.Sorted(maps.Keys(fullSections)) {
		all += fmt.Sprintf(`,%q:%s`, name, fullSections[name])
	}
	full := map[string]any{}
	for name, value := range fullSections {
		full[name] = decodeObject(t, value)
	}
	path := createConfigEntry(t, all)
	require.Equal(t, full, localSections(t, path))

	patchEntry(t, path, `{"name":"Renamed"}`)
	require.Equal(t, full, localSections(t, path), "omitted sections are kept")

	for _, name := range slices.Sorted(maps.Keys(fullSections)) {
		t.Run(name, func(t *testing.T) {
			path := createConfigEntry(t, all)
			patchEntry(t, path, fmt.Sprintf(`{%q:null}`, name))
			want := maps.Clone(full)
			delete(want, name)
			require.Equal(t, want, localSections(t, path), "null removes only that section")

			patchEntry(t, path, fmt.Sprintf(`{%q:{}}`, name))
			want[name] = map[string]any{}
			require.Equal(t, want, localSections(t, path), "an empty object stores an empty section")
		})
	}
}

// TestConfigPatchFields checks how PATCH changes the fields of a stored
// section. An omitted field is kept, null removes it, a nested object merges
// into the stored one and a list replaces the stored list.
func TestConfigPatchFields(t *testing.T) {
	resetDb()
	type patchCase struct {
		name  string
		patch string
		// want lists the fields that differ from the stored section after the
		// patch. A null value means the field is gone.
		want string
	}
	for _, tc := range []struct {
		section string
		stored  string
		cases   []patchCase
	}{
		{"illConfig", fullSections["illConfig"], []patchCase{
			{"scalars", `{"iso18626Url":"https://iso.example/b","includeSupplierInfo":true,"defaultLoanPeriod":0,"maxRequestsPerPatron":0,"minimumCost":0}`,
				`{"iso18626Url":"https://iso.example/b","includeSupplierInfo":true,"defaultLoanPeriod":0,"maxRequestsPerPatron":0,"minimumCost":0}`},
			{"code lists replace", `{"unfilledReasons":[{"code":"U2","defaultDesc":"Other"}],"loanConditions":[]}`,
				`{"unfilledReasons":[{"code":"U2","defaultDesc":"Other"}],"loanConditions":[]}`},
			{"default copyright type replaces", `{"defaultCopyrightType":{"code":"CC","defaultDesc":"Creative Commons"}}`,
				`{"defaultCopyrightType":{"code":"CC","defaultDesc":"Creative Commons"}}`},
			{"lenders are stored in upper case", `{"lendersOfLastResort":[{"authority":"test","symbol":"aninsttoo"}]}`,
				`{"lendersOfLastResort":[{"authority":"TEST","symbol":"ANINSTTOO"}]}`},
			{"empty lender list", `{"lendersOfLastResort":[]}`, `{"lendersOfLastResort":[]}`},
		}},
		{"lmsConfig", fullSections["lmsConfig"], []patchCase{
			{"scalars", `{"vendor":"Sierra","ncipVersion":"default","lookupUserEnabled":false,"itemLocation":""}`,
				`{"vendor":"Sierra","ncipVersion":"default","lookupUserEnabled":false,"itemLocation":""}`},
			{"empty address and fromAgency are unset", `{"address":"","fromAgency":""}`, `{"address":null,"fromAgency":null}`},
			{"patron profiles replace", `{"patronProfiles":[{"code":"UG","name":"Undergraduate","canCreateRequests":false}]}`,
				`{"patronProfiles":[{"code":"UG","name":"Undergraduate","canCreateRequests":false}]}`},
		}},
		{"catalogConfig", fullSections["catalogConfig"], []patchCase{
			{"scalars", `{"profile":"Sierra","metadataUpdateMode":"none"}`, `{"profile":"Sierra","metadataUpdateMode":"none"}`},
			{"sru record schema merges", `{"sru":{"recordSchema":"dc"}}`, `{"sru":{"address":"https://sru.example","recordSchema":"dc"}}`},
			{"sru address merges", `{"sru":{"address":"https://sru2.example"}}`, `{"sru":{"address":"https://sru2.example","recordSchema":"marcxml"}}`},
			{"query config merges", `{"queryConfig":{"isbn":"isbn={term}","type":"pqf"}}`,
				`{"queryConfig":{"type":"pqf","title":"title={term}","identifier":"rec.id={term}","identifierNormalization":"stripLeadingLetters","identifierPrefix":"x","isbn":"isbn={term}"}}`},
			{"empty query config", `{"queryConfig":{}}`, `{}`},
			{"metadata format merges", `{"metadataFormat":{"marc21":{"isbn":"020$a","title":""}}}`,
				`{"metadataFormat":{"marc21":{"title":"","author":"100$a","isbn":"020$a"}}}`},
			{"empty metadata format", `{"metadataFormat":{}}`, `{}`},
			{"empty marc21 metadata format", `{"metadataFormat":{"marc21":{}}}`, `{}`},
			{"same holdings parser merges", `{"holdingsFormat":{"marc":{"locationSubfield":"l"}}}`,
				`{"holdingsFormat":{"marc":{"mainField":"999","itemIdSubfield":"i","locationSubfield":"l","availability":[{"subfield":"a","operator":"equals","value":"y"}]}}}`},
			{"holdings availability list replaces", `{"holdingsFormat":{"marc":{"availability":[{"subfield":"b","operator":"absent"}]}}}`,
				`{"holdingsFormat":{"marc":{"mainField":"999","itemIdSubfield":"i","availability":[{"subfield":"b","operator":"absent"}]}}}`},
			{"different holdings parser replaces", `{"holdingsFormat":{"opac":{"trimWhitespace":true}}}`,
				`{"holdingsFormat":{"opac":{"trimWhitespace":true}}}`},
			{"empty parser selects it", `{"holdingsFormat":{"reservoir":{}}}`, `{"holdingsFormat":{"reservoir":{}}}`},
			{"empty holdings format", `{"holdingsFormat":{}}`, `{}`},
		}},
		{"catalogConfig", `{"profile":"Koha"}`, []patchCase{
			{"empty query config is not stored", `{"queryConfig":{}}`, `{}`},
			{"empty metadata format is not stored", `{"metadataFormat":{}}`, `{}`},
			{"empty marc21 metadata format is not stored", `{"metadataFormat":{"marc21":{}}}`, `{}`},
			{"empty holdings format is not stored", `{"holdingsFormat":{}}`, `{}`},
			{"empty parser is stored", `{"holdingsFormat":{"marc21plus1":{}}}`, `{"holdingsFormat":{"marc21plus1":{}}}`},
			{"zoom is created with its address", `{"zoom":{"address":"z.example:210/db","options":{"count":"10"}}}`,
				`{"zoom":{"address":"z.example:210/db","options":{"count":"10"}}}`},
		}},
		{"catalogConfig", `{"zoom":{"address":"z.example:210/db","options":{"count":"10","location":"MAIN"}}}`, []patchCase{
			{"zoom address merges", `{"zoom":{"address":"z2.example:210/db"}}`,
				`{"zoom":{"address":"z2.example:210/db","options":{"count":"10","location":"MAIN"}}}`},
			{"zoom options merge and null removes a key", `{"zoom":{"options":{"count":"20","location":null,"missing":null}}}`,
				`{"zoom":{"address":"z.example:210/db","options":{"count":"20"}}}`},
			{"zoom options can become empty", `{"zoom":{"options":{"count":null,"location":null}}}`,
				`{"zoom":{"address":"z.example:210/db","options":{}}}`},
		}},
		{"holdingsPolicy", fullSections["holdingsPolicy"], []patchCase{
			{"lists replace", `{"locations":[{"code":"BRANCH","name":"Branch","supplyPreference":1}],"itemLoanPolicies":[]}`,
				`{"locations":[{"code":"BRANCH","name":"Branch","supplyPreference":1}],"itemLoanPolicies":[]}`},
		}},
	} {
		stored := decodeObject(t, tc.stored)
		cases := tc.cases
		// The holdingsPolicy patch schema is the canonical one, which has no
		// nullable fields.
		for _, field := range slices.Sorted(maps.Keys(stored)) {
			if tc.section != "holdingsPolicy" {
				cases = append(cases, patchCase{field + " null", fmt.Sprintf(`{%q:null}`, field), fmt.Sprintf(`{%q:null}`, field)})
			}
		}
		cases = append(cases, patchCase{"empty patch", `{}`, `{}`})
		for _, pc := range cases {
			t.Run(tc.section+"/"+pc.name, func(t *testing.T) {
				path := createConfigEntry(t, fmt.Sprintf(`,%q:%s`, tc.section, tc.stored))
				patchEntry(t, path, fmt.Sprintf(`{%q:%s}`, tc.section, pc.patch))
				want := maps.Clone(stored)
				for field, value := range decodeObject(t, pc.want) {
					if value == nil {
						delete(want, field)
					} else {
						want[field] = value
					}
				}
				require.Equal(t, map[string]any{tc.section: want}, localSections(t, path))
			})
		}
	}
}

// TestConfigCreateUnsetsEmptyLmsStrings checks that create treats an empty
// LMS address or fromAgency as unset, as PATCH does.
func TestConfigCreateUnsetsEmptyLmsStrings(t *testing.T) {
	resetDb()
	path := createConfigEntry(t, `,"lmsConfig":{"address":"","fromAgency":"","itemLocation":""}`)
	require.Equal(t, map[string]any{"lmsConfig": map[string]any{"itemLocation": ""}}, localSections(t, path))
}

// TestConfigPatchEmptyHoldingsFormatKeepsInheritance checks that an empty
// holdingsFormat patch on an institution without a parser stores nothing, so
// its branch still sees no parser rather than an empty holdingsFormat.
func TestConfigPatchEmptyHoldingsFormatKeepsInheritance(t *testing.T) {
	tree := setupInheritanceTree(t)
	patchEntry(t, entryPath(tree.institution), `{"catalogConfig":{"profile":"Koha"}}`)
	patchEntry(t, entryPath(tree.institution), `{"catalogConfig":{"holdingsFormat":{}}}`)
	want := map[string]any{"profile": "Koha"}
	require.Equal(t, want, getEntryJSON(t, entryPath(tree.institution)+"?resolve-inherit=false", systemHeaders)["catalogConfig"])
	require.Equal(t, want, getEntryJSON(t, entryPath(tree.branch), systemHeaders)["catalogConfig"])

	patchEntry(t, entryPath(tree.institution), `{"catalogConfig":{"holdingsFormat":{"opac":{}}}}`)
	want["holdingsFormat"] = map[string]any{"opac": map[string]any{}}
	require.Equal(t, want, getEntryJSON(t, entryPath(tree.branch), systemHeaders)["catalogConfig"], "a stored parser is inherited")
	patchEntry(t, entryPath(tree.institution), `{"catalogConfig":{"holdingsFormat":{}}}`)
	require.Equal(t, want, getEntryJSON(t, entryPath(tree.branch), systemHeaders)["catalogConfig"], "an empty patch keeps the stored parser")
}
