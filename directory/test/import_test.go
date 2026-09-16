package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/directory/auth"
	"github.com/stretchr/testify/require"
)

func TestImportOrderedAggregates(t *testing.T) {
	resetImportState(t)
	consortium := symbolObject("ISIL", "CON")
	institution := symbolObject("ISIL", "INST")
	branch := symbolObject("ISIL", "BRANCH")
	records := []any{
		entryImportRecord(consortium, "Consortium", nil, "Consortium"),
		entryImportRecord(institution, "Institution", consortium, "Institution"),
		entryImportRecord(branch, "Branch", institution, "Branch"),
		map[string]any{"type": "tier", "key": map[string]any{"consortium": consortium, "name": "Loan"}, "data": map[string]any{"level": "standard", "type": "loan", "cost": 1.5, "entries": []any{institution, branch}}},
		map[string]any{"type": "network", "key": map[string]any{"consortium": consortium, "name": "Main"}, "data": map[string]any{"reciprocal": true, "entries": []any{
			map[string]any{"authority": "ISIL", "symbol": "INST", "priority": 7},
			map[string]any{"authority": "ISIL", "symbol": "BRANCH", "priority": 3},
		}}},
	}

	response, result := importRequest(t, records, "", standardHeaders)

	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, api.ImportSectionResult{Imported: 3}, result.Entries)
	require.Equal(t, api.ImportSectionResult{Imported: 1}, result.Tiers)
	require.Equal(t, api.ImportSectionResult{Imported: 1}, result.Networks)
	require.Empty(t, result.Errors)

	consortiumID := importedEntryID(t, "ISIL", "CON")
	institutionID := importedEntryID(t, "ISIL", "INST")
	branchID := importedEntryID(t, "ISIL", "BRANCH")
	require.NotEqual(t, uuid.Nil, consortiumID)
	require.NotEqual(t, consortiumID, institutionID)
	require.NotEqual(t, institutionID, branchID)
	var institutionParent, branchParent uuid.UUID
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT parent FROM entries WHERE id=$1`, institutionID).Scan(&institutionParent))
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT parent FROM entries WHERE id=$1`, branchID).Scan(&branchParent))
	require.Equal(t, consortiumID, institutionParent)
	require.Equal(t, institutionID, branchParent)

	for query, expected := range map[string]int{
		`SELECT count(*) FROM service_endpoints WHERE entry=$1`: 1,
		`SELECT count(*) FROM addresses WHERE entry=$1`:         1,
		`SELECT count(*) FROM closures WHERE entry=$1`:          1,
		`SELECT count(*) FROM lms_configs WHERE entry=$1`:       1,
	} {
		var count int
		require.NoError(t, dbpool.QueryRow(context.Background(), query, consortiumID).Scan(&count))
		require.Equal(t, expected, count)
	}
	var tierAssignments, networkAssignments int
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT count(*) FROM entry_tiers`).Scan(&tierAssignments))
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT count(*) FROM entry_networks`).Scan(&networkAssignments))
	require.Equal(t, 2, tierAssignments)
	require.Equal(t, 2, networkAssignments)
	var institutionPriority, branchPriority int32
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT priority FROM entry_networks WHERE entry=$1`, institutionID).Scan(&institutionPriority))
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT priority FROM entry_networks WHERE entry=$1`, branchID).Scan(&branchPriority))
	require.Equal(t, int32(7), institutionPriority)
	require.Equal(t, int32(3), branchPriority)
}

func TestImportPartialCommitAndConflictPolicies(t *testing.T) {
	resetImportState(t)
	consortium := symbolObject("ISIL", "CON")
	validInstitution := symbolObject("ISIL", "VALID")
	missing := symbolObject("ISIL", "MISSING")
	records := []any{
		entryImportRecord(consortium, "Consortium", nil, "Consortium"),
		entryImportRecord(symbolObject("ISIL", "BAD"), "Bad", missing, "Institution"),
		entryImportRecord(validInstitution, "Valid", consortium, "Institution"),
	}

	response, result := importRequest(t, records, "", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, api.ImportSectionResult{Imported: 2, Failed: 1}, result.Entries)
	require.Len(t, result.Errors, 1)
	require.Equal(t, int32(2), result.Errors[0].Line)
	require.Equal(t, 2, importedEntryCount(t))

	originalID := importedEntryID(t, "ISIL", "VALID")
	_, failed := importRequest(t, []any{entryImportRecord(validInstitution, "Failed", consortium, "Institution")}, "fail", standardHeaders)
	require.Equal(t, api.ImportSectionResult{Failed: 1}, failed.Entries)
	_, skipped := importRequest(t, []any{entryImportRecord(validInstitution, "Skipped", consortium, "Institution")}, "skip", standardHeaders)
	require.Equal(t, api.ImportSectionResult{Skipped: 1}, skipped.Entries)
	require.Len(t, skipped.Errors, 1)
	_, updated := importRequest(t, []any{entryImportRecord(validInstitution, "Updated", consortium, "Institution")}, "update", standardHeaders)
	require.Equal(t, api.ImportSectionResult{Imported: 1}, updated.Entries)
	require.Equal(t, originalID, importedEntryID(t, "ISIL", "VALID"))
	var name string
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT name FROM entries WHERE id=$1`, originalID).Scan(&name))
	require.Equal(t, "Updated", name)
}

func TestImportRequiresConsortialAdmin(t *testing.T) {
	for name, permission := range map[string]string{
		"institution": "directory.institution.all",
		"system":      "directory.system.all",
		"public":      "directory.public.all",
	} {
		t.Run(name, func(t *testing.T) {
			resetImportState(t)
			headers := map[string]string{auth.FolioPermissionsHeader: `[` + mustJSON(t, permission) + `]`}
			response, _ := importRequest(t, []any{entryImportRecord(symbolObject("ISIL", "CON"), "Consortium", nil, "Consortium")}, "", headers)
			require.Equal(t, http.StatusUnauthorized, response.StatusCode)
			require.Zero(t, importedEntryCount(t))
		})
	}
}

func importRequest(t *testing.T, records []any, policy string, headers map[string]string) (*http.Response, api.ImportResult) {
	t.Helper()
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)
	for _, record := range records {
		require.NoError(t, encoder.Encode(record))
	}
	path := appImportPath(policy)
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", "application/x-ndjson")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	response := recorder.Result()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	var result api.ImportResult
	if response.StatusCode == http.StatusOK {
		require.NoError(t, json.Unmarshal(data, &result), string(data))
	}
	return response, result
}

func appImportPath(policy string) string {
	path := "/directory/import"
	if policy != "" {
		path += "?conflictPolicy=" + url.QueryEscape(policy)
	}
	return path
}

func entryImportRecord(key map[string]any, name string, parent map[string]any, entryType string) map[string]any {
	data := map[string]any{
		"name": name, "type": entryType, "parent": parent, "description": nil, "organizationId": nil,
		"contactName": nil, "email": nil, "fromEmail": nil, "tenant": nil, "vendor": nil, "phoneNumber": nil,
		"lmsLocationCode": nil, "hrid": nil, "timeZone": nil, "symbols": []any{key},
		"endpoints": []any{}, "addresses": []any{}, "closures": []any{}, "lmsConfig": nil,
		"catalogConfig": nil, "illConfig": nil, "holdingsPolicy": nil,
	}
	if entryType == "Consortium" {
		data["endpoints"] = []any{map[string]any{"name": "ISO", "type": "ISO18626", "address": "https://example.test/ill"}}
		data["addresses"] = []any{map[string]any{"type": "Default", "addressComponents": []any{map[string]any{"seq": 1, "type": "Locality", "value": "Riga"}}}}
		data["closures"] = []any{map[string]any{"startDate": "2026-12-24", "endDate": "2026-12-26", "reason": "Holiday"}}
		data["lmsConfig"] = map[string]any{
			"vendor": nil, "ncipNamespaceEnabled": nil, "bibIdNormalization": nil,
			"address": "https://example.test/ncip", "fromAgency": "FROM", "fromAgencyAuthentication": "credential-value",
			"toAgency": nil, "lookupUserEnabled": nil, "acceptItemEnabled": nil, "checkInItemEnabled": nil,
			"checkOutItemEnabled": nil, "itemLocation": nil, "requestItemRequestType": nil,
			"requestItemRequestScopeType": nil, "requestItemBibIdCode": nil, "requestItemEnabled": nil,
			"requestItemPickupLocationEnabled": nil, "requesterPickupLocation": nil, "supplierPickupLocation": nil,
			"requesterPatronPattern": nil, "patronProfiles": nil,
		}
	}
	return map[string]any{"type": "entry", "key": key, "data": data}
}

func symbolObject(authority, symbol string) map[string]any {
	return map[string]any{"authority": authority, "symbol": symbol}
}

func resetImportState(t *testing.T) {
	t.Helper()
	_, err := dbpool.Exec(context.Background(), `TRUNCATE entries CASCADE`)
	require.NoError(t, err)
}

func importedEntryID(t *testing.T, authority, symbol string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT owner FROM symbols WHERE authority=$1 AND symbol=$2`, authority, symbol).Scan(&id))
	return id
}

func importedEntryCount(t *testing.T) int {
	t.Helper()
	var count int
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT count(*) FROM entries`).Scan(&count))
	return count
}

func mustJSON(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data)
}

func TestImportHostSettingsRoundTrip(t *testing.T) {
	resetImportState(t)
	record := entryImportRecord(symbolObject("ISIL", "HOST"), "Host settings", nil, "Consortium")
	data := record["data"].(map[string]any)
	lms := data["lmsConfig"].(map[string]any)
	lms["vendor"], lms["ncipNamespaceEnabled"], lms["bibIdNormalization"] = "Sierra", false, "none"
	// Selecting a profile without enabling NCIP is a valid complete import.
	lms["address"], lms["fromAgency"] = "", ""
	catalog := map[string]any{"profile": "Koha", "metadataUpdateMode": nil, "sru": nil, "zoom": nil, "queryConfig": nil, "metadataFormat": nil}
	data["catalogConfig"] = catalog
	for _, tc := range []struct {
		name, holdings, expected string
	}{
		{"MARC predicates", `{"marc":{"mainField":"999","callNumberSubField":null,"itemIdSubField":null,"locationSubField":null,"restrictedSubField":null,"shelvingLocationSubField":null,"availability":[{"subField":"7","operator":"equals","value":"0"},{"subField":"q","operator":"absent","value":null}]},"opac":null,"reservoir":null,"marc21plus1":null}`, `{"marc":{"mainField":"999","availability":[{"subField":"7","operator":"equals","value":"0"},{"subField":"q","operator":"absent"}]}}`},
		{"empty MARC predicates", `{"marc":{"mainField":null,"callNumberSubField":null,"itemIdSubField":null,"locationSubField":null,"restrictedSubField":null,"shelvingLocationSubField":null,"availability":[]},"opac":null,"reservoir":null,"marc21plus1":null}`, `{"marc":{"availability":[]}}`},
		{"OPAC overrides", `{"marc":null,"opac":{"availabilityRule":"publicNote","availablePublicNotes":["AVAILABLE"],"requireLocalLocation":false,"shelvingLocationSource":"localLocation","includeItemId":false,"includeItemLoanPolicy":false,"includeTemporaryLocation":true,"allCirculations":true},"reservoir":null,"marc21plus1":null}`, `{"opac":{"availabilityRule":"publicNote","availablePublicNotes":["AVAILABLE"],"requireLocalLocation":false,"shelvingLocationSource":"localLocation","includeItemId":false,"includeItemLoanPolicy":false,"includeTemporaryLocation":true,"allCirculations":true}}`},
		{"empty OPAC notes", `{"marc":null,"opac":{"availabilityRule":null,"availablePublicNotes":[],"requireLocalLocation":null,"shelvingLocationSource":null,"includeItemId":null,"includeItemLoanPolicy":null,"includeTemporaryLocation":null,"allCirculations":null},"reservoir":null,"marc21plus1":null}`, `{"opac":{"availablePublicNotes":[]}}`},
		{"reservoir", `{"marc":null,"opac":null,"reservoir":{},"marc21plus1":null}`, `{"reservoir":{}}`},
		{"marc21plus1", `{"marc":null,"opac":null,"reservoir":null,"marc21plus1":{}}`, `{"marc21plus1":{}}`},
		{"profile defaults", `{"marc":null,"opac":null,"reservoir":null,"marc21plus1":null}`, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var holdings map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.holdings), &holdings))
			catalog["holdingsFormat"] = holdings
			response, result := importRequest(t, []any{record}, "update", standardHeaders)
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Empty(t, result.Errors)
			require.Equal(t, int32(1), result.Entries.Imported)
			id := importedEntryID(t, "ISIL", "HOST")
			var vendor, normalization, profile string
			var namespace bool
			var raw []byte
			require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT vendor, ncip_namespace_enabled, bib_id_normalization FROM lms_configs WHERE entry=$1`, id).Scan(&vendor, &namespace, &normalization))
			require.Equal(t, "Sierra", vendor)
			require.False(t, namespace)
			require.Equal(t, "none", normalization)
			require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT profile, holdings_config FROM catalog_configs WHERE entry=$1`, id).Scan(&profile, &raw))
			require.Equal(t, "Koha", profile)
			require.JSONEq(t, tc.expected, string(raw))
			res, body := jsonReq(t, http.MethodGet, "/entries/by-id/"+id.String(), "", standardHeaders)
			require.Equal(t, http.StatusOK, res.StatusCode, body)
			var saved struct {
				LmsConfig struct {
					Vendor               string
					NcipNamespaceEnabled *bool
					BibIdNormalization   string
				}
				CatalogConfig struct {
					Profile        string
					HoldingsFormat json.RawMessage
				}
			}
			require.NoError(t, json.Unmarshal([]byte(body), &saved))
			require.Equal(t, "Sierra", saved.LmsConfig.Vendor)
			require.NotNil(t, saved.LmsConfig.NcipNamespaceEnabled)
			require.False(t, *saved.LmsConfig.NcipNamespaceEnabled)
			require.Equal(t, "none", saved.LmsConfig.BibIdNormalization)
			require.Equal(t, "Koha", saved.CatalogConfig.Profile)
			require.JSONEq(t, tc.expected, string(saved.CatalogConfig.HoldingsFormat))
		})
	}
	lms["vendor"], lms["ncipNamespaceEnabled"], lms["bibIdNormalization"] = nil, nil, nil
	catalog["profile"], catalog["holdingsFormat"] = nil, nil
	_, result := importRequest(t, []any{record}, "update", standardHeaders)
	require.Empty(t, result.Errors)
	id := importedEntryID(t, "ISIL", "HOST")
	var cleared bool
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT vendor IS NULL AND ncip_namespace_enabled IS NULL AND bib_id_normalization IS NULL FROM lms_configs WHERE entry=$1`, id).Scan(&cleared))
	require.True(t, cleared)
	require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT profile IS NULL AND holdings_config IS NULL FROM catalog_configs WHERE entry=$1`, id).Scan(&cleared))
	require.True(t, cleared)
}
