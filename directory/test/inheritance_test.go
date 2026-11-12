package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const fixtureConsortium = "00000000-0000-0000-0000-000000000004"

var (
	systemHeaders     = map[string]string{"X-Okapi-Permissions": `["directory.system.all"]`}
	publicHeaders     = map[string]string{"X-Okapi-Permissions": `["directory.public.all"]`}
	consortialHeaders = map[string]string{"X-Okapi-Tenant": "INHERIT", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	ownerHeaders      = map[string]string{"X-Okapi-Tenant": "INHERIT", "X-Okapi-Permissions": `["directory.institution.all"]`}
)

// inheritanceTree is an institution and branch under the fixture consortium,
// owned by tenant INHERIT.
type inheritanceTree struct {
	institution, branch uuid.UUID
}

func setupInheritanceTree(t *testing.T) inheritanceTree {
	t.Helper()
	resetDb()
	// Import keys as IDs let import tests update the same entries.
	tree := inheritanceTree{
		institution: importEntryUUID(symbolObject("ISIL", "INHERIT-INST")),
		branch:      importEntryUUID(symbolObject("ISIL", "INHERIT-BRANCH")),
	}
	// Fixture loading deletes entries without cascading, so clear their
	// configuration explicitly.
	ids := []uuid.UUID{tree.institution, tree.branch, uuid.MustParse(fixtureConsortium)}
	for _, table := range []string{"ill_configs", "lms_configs", "catalog_configs", "symbols"} {
		column := "entry"
		if table == "symbols" {
			column = "owner"
		}
		sqlExec(t, fmt.Sprintf(`DELETE FROM %s WHERE %s = ANY($1)`, table, column), ids)
	}
	// The fixture consortium outlives this test; keep its settings from
	// leaking into other tests' merged reads.
	t.Cleanup(func() {
		sqlExec(t, `DELETE FROM ill_configs WHERE entry = $1`, fixtureConsortium)
		sqlExec(t, `DELETE FROM catalog_configs WHERE entry = $1`, fixtureConsortium)
	})
	sqlExec(t, `INSERT INTO entries (id, name, type, parent, tenant) VALUES ($1, 'Inherit institution', 'Institution', $3, 'INHERIT'), ($2, 'Inherit branch', 'Branch', $1, 'INHERIT')`,
		tree.institution, tree.branch, fixtureConsortium)
	sqlExec(t, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'ISIL', 'INHERIT-INST'), ($2, 'ISIL', 'INHERIT-BRANCH')`, tree.institution, tree.branch)
	sqlExec(t, `INSERT INTO ill_configs (entry, max_requests_per_patron, iso18626_url, default_loan_period, lenders_of_last_resort, is_pickup_location, load_balancing_policy)
		VALUES ($1, 10, 'https://consortium.example/iso', 30, ARRAY['ISIL:LAST'], TRUE, 'deficit')`, fixtureConsortium)
	sqlExec(t, `INSERT INTO ill_configs (entry, iso18626_url) VALUES ($1, 'https://institution.example/iso')`, tree.institution)
	sqlExec(t, `INSERT INTO lms_configs (entry, vendor, address, from_agency, from_agency_authentication, requester_pickup_location)
		VALUES ($1, 'Horizon', 'https://ncip.example', 'INST', 'secret', 'MAIN')`, tree.institution)
	sqlExec(t, `INSERT INTO lms_configs (entry, address, from_agency, requester_pickup_location) VALUES ($1, '', '', 'BR1')`, tree.branch)
	return tree
}

func sqlExec(t *testing.T, query string, args ...any) {
	t.Helper()
	_, err := dbpool.Exec(context.Background(), query, args...)
	require.NoError(t, err)
}

func getEntryJSON(t *testing.T, path string, headers map[string]string) map[string]any {
	t.Helper()
	response, body := jsonReq(t, http.MethodGet, path, "", headers)
	require.Equal(t, http.StatusOK, response.StatusCode, body)
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &entry))
	return entry
}

func entryPath(id uuid.UUID) string {
	return "/entries/by-id/" + id.String()
}

func TestInheritanceMergedAndLocalReads(t *testing.T) {
	tree := setupInheritanceTree(t)

	institution := getEntryJSON(t, entryPath(tree.institution), systemHeaders)
	require.Equal(t, map[string]any{
		"maxRequestsPerPatron": float64(10),
		"defaultLoanPeriod":    float64(30),
		"lendersOfLastResort":  []any{map[string]any{"authority": "ISIL", "symbol": "LAST"}},
		"iso18626Url":          "https://institution.example/iso",
	}, institution["illConfig"], "consortium ILL settings inherit, except the ISO18626 peer, pickup flag and load balancing")

	local := getEntryJSON(t, entryPath(tree.institution)+"?resolve-inherit=false", systemHeaders)
	require.Equal(t, map[string]any{"iso18626Url": "https://institution.example/iso"}, local["illConfig"])

	wantBranchLms := map[string]any{
		"vendor": "Horizon", "address": "https://ncip.example", "fromAgency": "INST", "fromAgencyAuthentication": "secret",
		"requesterPickupLocation": "BR1",
	}
	branch := getEntryJSON(t, entryPath(tree.branch), systemHeaders)
	require.Equal(t, wantBranchLms, branch["lmsConfig"], "the branch inherits its institution's LMS settings but keeps its pickup code")
	require.Equal(t, "https://institution.example/iso", branch["illConfig"].(map[string]any)["iso18626Url"])
	require.Equal(t, map[string]any{"requesterPickupLocation": "BR1"},
		getEntryJSON(t, entryPath(tree.branch)+"?resolve-inherit=false", systemHeaders)["lmsConfig"])

	// Every read path returns the same merged entry.
	require.Equal(t, branch, getEntryJSON(t, "/entries/by-symbol/ISIL:INHERIT-BRANCH", systemHeaders))
	cql := url.QueryEscape(fmt.Sprintf(`parent=%s`, tree.institution))
	for _, path := range []string{"/entries?cql=" + cql, "/entries/owned?cql=" + cql} {
		list := getEntryJSON(t, path, map[string]string{"X-Okapi-Tenant": "INHERIT", "X-Okapi-Permissions": `["directory.system.all"]`})
		require.Equal(t, []any{branch}, list["items"], path)
		require.Equal(t, float64(1), list["about"].(map[string]any)["count"], path)
	}
	list := getEntryJSON(t, "/entries?resolve-inherit=false&cql="+cql, systemHeaders)
	require.Equal(t, map[string]any{"requesterPickupLocation": "BR1"}, list["items"].([]any)[0].(map[string]any)["lmsConfig"])

	// A parent change takes effect on the next read without touching the child.
	sqlExec(t, `UPDATE lms_configs SET item_location = 'STACKS' WHERE entry = $1`, tree.institution)
	require.Equal(t, "STACKS", getEntryJSON(t, entryPath(tree.branch), systemHeaders)["lmsConfig"].(map[string]any)["itemLocation"])
}

func TestInheritanceOverridesOffValuesAndEmptyArrays(t *testing.T) {
	tree := setupInheritanceTree(t)
	path := entryPath(tree.institution)
	response, body := jsonReq(t, http.MethodPatch, path, `{"illConfig":{"maxRequestsPerPatron":25,"defaultLoanPeriod":0,"lendersOfLastResort":[]}}`, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)

	for _, id := range []uuid.UUID{tree.institution, tree.branch} {
		illConfig := getEntryJSON(t, entryPath(id), systemHeaders)["illConfig"].(map[string]any)
		require.Equal(t, float64(25), illConfig["maxRequestsPerPatron"], "a local value overrides the consortium")
		require.NotContains(t, illConfig, "defaultLoanPeriod", "0 turns the fallback off and is omitted, even below the institution")
		require.Equal(t, []any{}, illConfig["lendersOfLastResort"], "an empty list blocks inheritance")
	}
	require.Equal(t, float64(0), getEntryJSON(t, path+"?resolve-inherit=false", systemHeaders)["illConfig"].(map[string]any)["defaultLoanPeriod"])

	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"maxRequestsPerPatron":null,"defaultLoanPeriod":null,"lendersOfLastResort":null}}`, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	illConfig := getEntryJSON(t, path, systemHeaders)["illConfig"].(map[string]any)
	require.Equal(t, float64(10), illConfig["maxRequestsPerPatron"], "null restores inheritance")
	require.Equal(t, float64(30), illConfig["defaultLoanPeriod"])
	require.Len(t, illConfig["lendersOfLastResort"], 1)
}

// TestInheritanceCatalogNullClears checks that null on a catalog sub-object
// clears only that sub-object, including legacy holdings columns, and that the
// branch's merged view follows the institution.
func TestInheritanceCatalogNullClears(t *testing.T) {
	tree := setupInheritanceTree(t)
	// The consortium's catalog settings never flow to institutions.
	sqlExec(t, `INSERT INTO catalog_configs (entry, sru_address, query_title, metadata_marc21_title)
		VALUES ($1, 'https://consortium.example/sru', 'consortium={term}', '246')`, fixtureConsortium)
	// The institution's holdings are stored in both representations. Reads
	// prefer the JSON, so leftover JSON or legacy columns show after a clear.
	sqlExec(t, `INSERT INTO catalog_configs (entry, profile, metadata_update_mode, sru_address, sru_record_schema,
		query_type, query_title, holdings_config, holdings_marc_main_field, holdings_marc_call_number_subfield, metadata_marc21_author)
		VALUES ($1, 'Koha', 'merge', 'https://institution.example/sru', 'marcxml', 'cql', 'title={term}',
		'{"marc":{"mainField":"999"}}', '852', 'h', '100')`, tree.institution)
	path := entryPath(tree.institution)
	catalog := func(id uuid.UUID, query string) any {
		return getEntryJSON(t, entryPath(id)+query, systemHeaders)["catalogConfig"]
	}
	patch := func(body string, status int) {
		t.Helper()
		response, data := jsonReq(t, http.MethodPatch, path, `{"catalogConfig":`+body+`}`, consortialHeaders)
		require.Equal(t, status, response.StatusCode, data)
	}
	requireViews := func(want map[string]any, msg string) {
		t.Helper()
		require.Equal(t, want, catalog(tree.institution, "?resolve-inherit=false"), msg)
		require.Equal(t, want, catalog(tree.institution, ""), msg+": institution merged view")
		require.Equal(t, want, catalog(tree.branch, ""), msg+": branch merged view")
	}
	want := map[string]any{
		"profile":            "Koha",
		"metadataUpdateMode": "merge",
		"sru":                map[string]any{"address": "https://institution.example/sru", "recordSchema": "marcxml"},
		"queryConfig":        map[string]any{"type": "cql", "title": "title={term}"},
		"holdingsFormat":     map[string]any{"marc": map[string]any{"mainField": "999"}},
		"metadataFormat":     map[string]any{"marc21": map[string]any{"author": "100"}},
	}
	requireViews(want, "the branch inherits the institution's catalog settings")
	require.Nil(t, catalog(tree.branch, "?resolve-inherit=false"))

	// Switching endpoints needs null: SRU and ZOOM can't both be stored.
	patch(`{"zoom":{"address":"z3950.institution.example:210/db"}}`, http.StatusBadRequest)
	patch(`{"sru":null,"zoom":{"address":"z3950.institution.example:210/db"}}`, http.StatusNoContent)
	delete(want, "sru")
	want["zoom"] = map[string]any{"address": "z3950.institution.example:210/db"}
	requireViews(want, "null clears sru and keeps the other settings")

	patch(`{"queryConfig":null,"holdingsFormat":null,"metadataFormat":null,"metadataUpdateMode":null}`, http.StatusNoContent)
	want = map[string]any{"profile": "Koha", "zoom": want["zoom"]}
	requireViews(want, "null clears each sub-object, including both holdings representations; consortium values stay out")

	// A new holdings parser doesn't merge with the cleared one.
	patch(`{"holdingsFormat":{"opac":{}}}`, http.StatusNoContent)
	want["holdingsFormat"] = map[string]any{"opac": map[string]any{}}
	requireViews(want, "holdings set after a clear start empty")

	patch(`{"zoom":null}`, http.StatusNoContent)
	delete(want, "zoom")
	requireViews(want, "null clears zoom")
}

func TestInheritanceEditability(t *testing.T) {
	tree := setupInheritanceTree(t)
	branchPath := entryPath(tree.branch)
	patch := func(path, body string) (int, string) {
		t.Helper()
		response, data := jsonReq(t, http.MethodPatch, path, body, consortialHeaders)
		return response.StatusCode, data
	}

	status, body := patch(branchPath, `{"lmsConfig":{"requesterPickupLocation":"BR2"},"illConfig":{"isPickupLocation":true}}`)
	require.Equal(t, http.StatusNoContent, status, body)

	status, body = patch(branchPath, `{"illConfig":{"isPickupLocation":false,"maxRequestsPerPatron":5}}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "illConfig.maxRequestsPerPatron is not editable on Branch", body)
	local := getEntryJSON(t, branchPath+"?resolve-inherit=false", systemHeaders)
	require.Equal(t, map[string]any{"isPickupLocation": true}, local["illConfig"], "a rejected request changes nothing")

	status, body = patch("/entries/by-id/"+fixtureConsortium, `{"lmsConfig":{"vendor":"Koha"}}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "lmsConfig.vendor is not editable on Consortium", body)

	response, body := jsonReq(t, http.MethodPost, "/entries", fmt.Sprintf(`{"name":"New branch","type":"Branch","parent":"%s","lmsConfig":{"address":"https://other.example"}}`, tree.institution), consortialHeaders)
	require.Equal(t, http.StatusBadRequest, response.StatusCode)
	require.Equal(t, "lmsConfig.address is not editable on Branch", body)

	// A legacy branch value supplies its whole merge unit and never mixes with
	// the institution's credentials.
	sqlExec(t, `UPDATE lms_configs SET address = 'https://legacy.example', from_agency = 'BR' WHERE entry = $1`, tree.branch)
	require.Equal(t, map[string]any{"address": "https://legacy.example", "fromAgency": "BR", "requesterPickupLocation": "BR2"},
		getEntryJSON(t, branchPath, systemHeaders)["lmsConfig"])

	status, body = patch(branchPath, `{"lmsConfig":{"address":"https://legacy.example"}}`)
	require.Equal(t, http.StatusNoContent, status, "resubmitting a stored value is a no-op: %s", body)
	status, body = patch(branchPath, `{"lmsConfig":{"address":"https://new.example"}}`)
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "lmsConfig.address is not editable on Branch", body)

	status, body = patch(branchPath, `{"lmsConfig":{"address":null,"fromAgency":null}}`)
	require.Equal(t, http.StatusNoContent, status, "null clears a value the entry type may not set: %s", body)
	require.Equal(t, "https://ncip.example", getEntryJSON(t, branchPath, systemHeaders)["lmsConfig"].(map[string]any)["address"])
}

func TestInheritanceProtectedValuesAndReparenting(t *testing.T) {
	tree := setupInheritanceTree(t)
	branchPath := entryPath(tree.branch)

	credential := func(headers map[string]string) any {
		t.Helper()
		return getEntryJSON(t, branchPath, headers)["lmsConfig"].(map[string]any)["fromAgencyAuthentication"]
	}
	require.Equal(t, "secret", credential(ownerHeaders), "the owner sees a credential inherited within its tenant")
	require.Equal(t, "", credential(publicHeaders), "other callers see the existing redaction")
	require.Equal(t, "", credential(map[string]string{"X-Okapi-Tenant": "OTHER", "X-Okapi-Permissions": `["directory.institution.all"]`}))

	other := uuid.New()
	sqlExec(t, `INSERT INTO entries (id, name, type, parent, tenant) VALUES ($1, 'Other institution', 'Institution', $2, 'OTHER')`, other, fixtureConsortium)
	sqlExec(t, `INSERT INTO lms_configs (entry, address, from_agency, from_agency_authentication) VALUES ($1, 'https://other.example', 'OTHER', 'other-secret')`, other)

	reparent := fmt.Sprintf(`{"parent":"%s"}`, other)
	response, body := jsonReq(t, http.MethodPatch, branchPath, reparent, ownerHeaders)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	require.Equal(t, hierarchyDenied, body)
	require.Equal(t, tree.institution.String(), getEntryJSON(t, branchPath, systemHeaders)["parent"])

	response, body = jsonReq(t, http.MethodPatch, branchPath, reparent, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	lms := getEntryJSON(t, branchPath, systemHeaders)["lmsConfig"].(map[string]any)
	require.Equal(t, "https://other.example", lms["address"], "reparenting changes inherited values on the next read")
	require.NotContains(t, lms, "fromAgencyAuthentication", "protected values never cross a tenant boundary")
}

const hierarchyDenied = "Only consortial administrators may change an entry's parent or type"

// TestOnlyConsortialAdminsChangeHierarchy checks that institutional admins
// can't change an entry's parent or type, even within their own tenant, but
// may resend the stored values.
func TestOnlyConsortialAdminsChangeHierarchy(t *testing.T) {
	tree := setupInheritanceTree(t)
	owned := uuid.New()
	sqlExec(t, `INSERT INTO entries (id, name, type, parent, tenant) VALUES ($1, 'Second institution', 'Institution', $2, 'INHERIT')`, owned, fixtureConsortium)
	branchPath := entryPath(tree.branch)
	requireHierarchy := func(parent, entryType string, msg string) {
		t.Helper()
		local := getEntryJSON(t, branchPath+"?resolve-inherit=false", systemHeaders)
		require.Equal(t, parent, local["parent"], msg)
		require.Equal(t, entryType, local["type"], msg)
	}

	for _, tc := range []struct{ name, path, body string }{
		{"move between owned institutions", branchPath, fmt.Sprintf(`{"parent":"%s"}`, owned)},
		{"move by symbol", "/entries/by-symbol/ISIL:INHERIT-BRANCH", fmt.Sprintf(`{"parent":"%s"}`, owned)},
		{"detach", branchPath, `{"parent":null}`},
		{"change type only", branchPath, `{"type":"Institution"}`},
		{"change type and parent", branchPath, `{"type":"Institution","parent":"` + fixtureConsortium + `"}`},
		{"change type and detach", branchPath, `{"type":"Institution","parent":null}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, body := jsonReq(t, http.MethodPatch, tc.path, tc.body, ownerHeaders)
			require.Equal(t, http.StatusUnauthorized, response.StatusCode, body)
			require.Equal(t, hierarchyDenied, body)
			requireHierarchy(tree.institution.String(), "Branch", "a rejected request changes nothing")
		})
	}

	resend := fmt.Sprintf(`{"name":"Renamed branch","type":"Branch","parent":"%s"}`, tree.institution)
	response, body := jsonReq(t, http.MethodPatch, branchPath, resend, ownerHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, "Renamed branch", getEntryJSON(t, branchPath, systemHeaders)["name"], "stored parent and type may be resent")
	requireHierarchy(tree.institution.String(), "Branch", "resending keeps the hierarchy")

	response, body = jsonReq(t, http.MethodPatch, branchPath, fmt.Sprintf(`{"parent":"%s"}`, owned), consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	requireHierarchy(owned.String(), "Branch", "consortial admins may move a branch")
	response, body = jsonReq(t, http.MethodPatch, branchPath, `{"type":"Institution","parent":"`+fixtureConsortium+`"}`, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	requireHierarchy(fixtureConsortium, "Institution", "consortial admins may change the type")
}

func TestImportInheritanceRules(t *testing.T) {
	tree := setupInheritanceTree(t)
	branchKey := symbolObject("ISIL", "INHERIT-BRANCH")
	institutionKey := symbolObject("ISIL", "INHERIT-INST")
	sqlExec(t, `UPDATE lms_configs SET address = 'https://legacy.example', from_agency = 'BR' WHERE entry = $1`, tree.branch)
	branchID := tree.branch

	record := entryImportRecord(branchKey, "Inherit branch", institutionKey, "Branch")
	data := record["data"].(map[string]any)
	data["tenant"] = "INHERIT"
	lms := importLmsConfig()
	lms["address"], lms["fromAgency"], lms["fromAgencyAuthentication"] = nil, nil, nil
	lms["requesterPickupLocation"] = "BR1"
	data["lmsConfig"] = lms

	data["lmsConfig"] = importLmsConfig()
	response, result := importRequest(t, []any{record}, "update", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "lmsConfig.address is not editable on Branch; lmsConfig.fromAgency is not editable on Branch; lmsConfig.fromAgencyAuthentication is not editable on Branch", result.Errors[0].Error)

	// Resubmitting the stored legacy values is not a change, so it is accepted.
	legacy := importLmsConfig()
	legacy["address"], legacy["fromAgency"], legacy["fromAgencyAuthentication"] = "https://legacy.example", "BR", nil
	legacy["requesterPickupLocation"] = "BR1"
	data["lmsConfig"] = legacy
	response, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Empty(t, result.Errors)
	require.Equal(t, "https://legacy.example", getEntryJSON(t, entryPath(branchID), systemHeaders)["lmsConfig"].(map[string]any)["address"])

	data["lmsConfig"] = lms
	response, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Empty(t, result.Errors)
	var address, fromAgency string
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT address, from_agency FROM lms_configs WHERE entry = $1`, branchID).Scan(&address, &fromAgency))
	require.Equal(t, []string{"", ""}, []string{address, fromAgency}, "an import null clears a legacy value")
	merged := getEntryJSON(t, entryPath(branchID), systemHeaders)["lmsConfig"].(map[string]any)
	require.Equal(t, "https://ncip.example", merged["address"])
	require.Equal(t, "BR1", merged["requesterPickupLocation"])
}

// TestImportResubmitsStoredConfiguration imports a fully configured
// institution, then re-imports the same record as a branch. Every value the
// branch may not set must compare equal to what is stored, so the import
// succeeds; changing one of them fails the record.
func TestImportResubmitsStoredConfiguration(t *testing.T) {
	tree := setupInheritanceTree(t)
	key := symbolObject("ISIL", "ROUND-TRIP")
	record := institutionImportRecord(key, "Round trip")
	data := record["data"].(map[string]any)
	data["parent"] = fixtureConsortium
	data["tenant"] = "INHERIT"
	lms := data["lmsConfig"].(map[string]any)
	lms["vendor"], lms["ncipVersion"], lms["ncipNamespaceEnabled"] = "Sierra", "2.02", false
	lms["requesterPickupLocation"] = "RT"
	lms["patronProfiles"] = []any{map[string]any{"code": "STAFF", "name": "Staff", "canCreateRequests": true}}
	data["illConfig"] = map[string]any{
		"iso18626Url": "https://round-trip.example/iso", "iso18626Vendor": nil, "lendersOfLastResort": []any{tree.institution},
		"includeRequestingAgencyInfo": true, "includeSupplierInfo": false, "includeReturnInfo": nil,
		"includeVendorNote": nil, "useOfferedCosts": nil, "noteFieldSeparator": " / ",
		"supplierPatronPattern": nil, "duplicateCheckWindowHours": 0, "defaultLoanPeriod": 0, "maxRequestsPerPatron": 5,
		"minimumCost": 0, "isPickupLocation": true,
		"defaultCopyrightType": map[string]any{"code": "US_CCG", "defaultDesc": "US copyright guidelines"},
		"loanConditions":       []any{map[string]any{"code": "LibraryUseOnly", "defaultDesc": "Library use only"}},
		"unfilledReasons":      []any{}, "cancellationReasons": nil,
	}
	var catalog map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"profile":"Koha","metadataUpdateMode":"merge",
		"sru":{"address":"https://round-trip.example/sru","recordSchema":null},"zoom":null,
		"queryConfig":{"type":"cql","identifier":null,"isbn":"isbn={term}","issn":null,"title":null},
		"holdingsFormat":{"marc":{"mainField":"999","callNumberSubfield":null,"itemIdSubfield":null,"locationSubfield":null,"restrictedSubfield":null,"shelvingLocationSubfield":null,"availability":[{"subfield":"7","operator":"equals","value":"0"},{"subfield":"q","operator":"absent","value":null}]},"opac":null,"reservoir":null,"marc21plus1":null},
		"metadataFormat":{"marc21":{"author":"100","edition":null,"identifier":null,"isbn":null,"issn":null,"subtitle":null,"title":"245"}}}`), &catalog))
	data["catalogConfig"] = catalog
	data["holdingsPolicy"] = map[string]any{
		"locations":         []any{map[string]any{"code": "MAIN", "name": "Main", "supplyPreference": 5}},
		"shelvingLocations": []any{}, "locationPolicies": []any{},
		"itemLoanPolicies": []any{map[string]any{"code": "NORMAL", "name": "Normal", "lendable": true}},
	}
	_, result := importRequest(t, []any{record}, "fail", standardHeaders)
	require.Empty(t, result.Errors)
	path := "/entries/by-id/" + importEntryUUID(key).String() + "?resolve-inherit=false"
	stored := getEntryJSON(t, path, systemHeaders)

	data["type"], data["parent"] = "Branch", tree.institution
	_, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Empty(t, result.Errors, "unchanged values compare equal to the stored configuration")
	branch := getEntryJSON(t, path, systemHeaders)
	require.Equal(t, "Branch", branch["type"])
	for _, container := range []string{"lmsConfig", "illConfig", "catalogConfig", "holdingsPolicy"} {
		require.Equal(t, stored[container], branch[container], container)
	}

	data["illConfig"].(map[string]any)["maxRequestsPerPatron"] = 6
	data["illConfig"].(map[string]any)["lendersOfLastResort"] = []any{}
	_, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "illConfig.lendersOfLastResort is not editable on Branch; illConfig.maxRequestsPerPatron is not editable on Branch", result.Errors[0].Error)
	require.Equal(t, stored["illConfig"], getEntryJSON(t, path, systemHeaders)["illConfig"], "a rejected record changes nothing")
}

func TestInheritanceOffValueMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(query string, args ...any) error {
		_, err := tx.Exec(ctx, query, args...)
		return err
	}
	require.NoError(t, exec("CREATE SCHEMA off_value_migration_test; SET LOCAL search_path TO off_value_migration_test"))
	migration := func(version int, direction string) string {
		t.Helper()
		paths, err := filepath.Glob(fmt.Sprintf("../migrations/%03d_*.%s.sql", version, direction))
		require.NoError(t, err)
		require.Len(t, paths, 1)
		sql, err := os.ReadFile(paths[0])
		require.NoError(t, err)
		return string(sql)
	}
	for version := 1; version <= 25; version++ {
		require.NoError(t, exec(migration(version, "up")), version)
	}
	id := uuid.New()
	require.NoError(t, exec(`INSERT INTO entries (id, name, type) VALUES ($1, 'Off values', 'Institution')`, id))
	require.NoError(t, exec(`INSERT INTO ill_configs (entry, default_loan_period) VALUES ($1, 0)`, id))
	require.NoError(t, exec(`INSERT INTO lms_configs (entry, address, from_agency, ncip_version) VALUES ($1, '', '', 'default')`, id))
	require.NoError(t, exec("SAVEPOINT invalid"))
	require.Error(t, exec(`UPDATE lms_configs SET ncip_version = 'other' WHERE entry = $1`, id))
	require.NoError(t, exec("ROLLBACK TO SAVEPOINT invalid"))

	require.NoError(t, exec(migration(25, "down")))
	var period *int32
	var version *string
	require.NoError(t, tx.QueryRow(ctx, `SELECT default_loan_period FROM ill_configs WHERE entry = $1`, id).Scan(&period))
	require.NoError(t, tx.QueryRow(ctx, `SELECT ncip_version FROM lms_configs WHERE entry = $1`, id).Scan(&version))
	require.Nil(t, period, "the down migration maps off values to the earlier null meaning")
	require.Nil(t, version)
	require.NoError(t, exec("SAVEPOINT restored"))
	require.Error(t, exec(`UPDATE ill_configs SET default_loan_period = 0 WHERE entry = $1`, id), "the earlier constraint is restored")
	require.NoError(t, exec("ROLLBACK TO SAVEPOINT restored"))
}
