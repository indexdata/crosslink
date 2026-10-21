package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/indexdata/crosslink/directory/auth"
	"github.com/stretchr/testify/require"
)

func TestIllConfigListsPersistence(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	response, body := jsonReq(t, http.MethodPost, "/entries", `{"name":"ILL configuration lists","type":"Institution","parent":"00000000-0000-0000-0000-000000000004","illConfig":{"availableCopyrightTypes":[{"code":"CustomCopyright-B","defaultDesc":"Second copyright"},{"code":"CustomCopyright-A","defaultDesc":"First copyright"}],"unfilledReasons":[{"code":"Legacy-B","defaultDesc":"Second & quoted \"reason\""},{"code":"Legacy-A","defaultDesc":"First"}],"cancellationReasons":[{"code":"CustomCancel","defaultDesc":"Cancelled"}],"loanConditions":[{"code":"LegacyCondition-B","defaultDesc":"Supervised & quoted \"use\""},{"code":"LegacyCondition-A","defaultDesc":"First condition"}]}}`, headers)
	require.Equal(t, http.StatusCreated, response.StatusCode, body)
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &entry))
	path := "/entries/by-id/" + entry["id"].(string)
	read := func() map[string]any {
		t.Helper()
		response, body := jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		require.NoError(t, json.Unmarshal([]byte(body), &entry))
		return entry["illConfig"].(map[string]any)
	}
	original := read()
	require.Equal(t, []any{map[string]any{"code": "CustomCopyright-B", "defaultDesc": "Second copyright"}, map[string]any{"code": "CustomCopyright-A", "defaultDesc": "First copyright"}}, original["availableCopyrightTypes"])
	require.Len(t, original["unfilledReasons"], 2)
	require.Equal(t, []any{map[string]any{"code": "LegacyCondition-B", "defaultDesc": "Supervised & quoted \"use\""}, map[string]any{"code": "LegacyCondition-A", "defaultDesc": "First condition"}}, original["loanConditions"])
	require.Equal(t, "Legacy-B", original["unfilledReasons"].([]any)[0].(map[string]any)["code"])
	require.Equal(t, "Second & quoted \"reason\"", original["unfilledReasons"].([]any)[0].(map[string]any)["defaultDesc"])
	require.Equal(t, "CustomCancel", original["cancellationReasons"].([]any)[0].(map[string]any)["code"])
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"useOfferedCosts":true}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	current := read()
	require.Equal(t, original["availableCopyrightTypes"], current["availableCopyrightTypes"])
	require.Equal(t, original["unfilledReasons"], current["unfilledReasons"])
	require.Equal(t, original["cancellationReasons"], current["cancellationReasons"])
	require.Equal(t, original["loanConditions"], current["loanConditions"])
	for _, field := range []string{"availableCopyrightTypes", "unfilledReasons", "cancellationReasons", "loanConditions"} {
		for _, invalid := range []string{`null`, `"invalid"`, `[null]`, `[{"code":"A"}]`, `[{"code":"A","defaultDesc":2}]`, `[{"code":"A","defaultDesc":"B","extra":true}]`} {
			response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"`+field+`":`+invalid+`}}`, headers)
			require.Equal(t, http.StatusBadRequest, response.StatusCode, body)
			require.Equal(t, current, read(), "rejected PATCH preserves configuration")
		}
	}
	for _, field := range []string{"availableCopyrightTypes", "unfilledReasons", "cancellationReasons", "loanConditions"} {
		response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"`+field+`":[{"code":"NewCustom","defaultDesc":"Replacement"}]}}`, headers)
		require.Equal(t, http.StatusNoContent, response.StatusCode, body)
		current[field] = []any{map[string]any{"code": "NewCustom", "defaultDesc": "Replacement"}}
		require.Equal(t, current, read(), "PATCH replaces only the supplied list")
		response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"`+field+`":[]}}`, headers)
		require.Equal(t, http.StatusNoContent, response.StatusCode, body)
		current[field] = []any{}
		require.Equal(t, current, read(), "clearing one list preserves the others")
	}
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":null}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	var count int
	require.NoError(t, dbpool.QueryRow(context.Background(), "SELECT count(*) FROM ill_configs WHERE entry=$1", entry["id"]).Scan(&count))
	require.Zero(t, count)
}

func TestImportIllConfigLists(t *testing.T) {
	resetImportState(t)
	consortium := symbolObject("ISIL", "CON")
	record := entryImportRecord(consortium, "Consortium", nil, "Consortium")
	config := map[string]any{
		"iso18626Url": nil, "iso18626Vendor": nil, "lendersOfLastResort": []any{},
		"includeRequestingAgencyInfo": nil, "includeSupplierInfo": nil, "includeReturnInfo": nil,
		"includeVendorNote": nil, "useOfferedCosts": nil, "noteFieldSeparator": nil,
		"supplierPatronPattern": nil, "duplicateCheckWindowHours": nil,
		"unfilledReasons":     []any{map[string]any{"code": "Legacy-B", "defaultDesc": "Second"}, map[string]any{"code": "Legacy-A", "defaultDesc": "First"}},
		"cancellationReasons": []any{map[string]any{"code": "CustomCancel", "defaultDesc": "Cancelled"}},
		"loanConditions":      []any{map[string]any{"code": "LegacyCondition-B", "defaultDesc": "Supervised use"}, map[string]any{"code": "LegacyCondition-A", "defaultDesc": "No copying"}},
	}
	data := record["data"].(map[string]any)
	data["illConfig"] = config
	_, result := importRequest(t, []any{record}, "", standardHeaders)
	require.Empty(t, result.Errors)
	require.EqualValues(t, 1, result.Entries.Imported)
	path := "/entries/by-id/" + importEntryUUID(consortium).String()
	read := func() map[string]any {
		t.Helper()
		response, body := jsonReq(t, http.MethodGet, path, "", standardHeaders)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &entry))
		return entry["illConfig"].(map[string]any)
	}
	require.Equal(t, config["unfilledReasons"], read()["unfilledReasons"])
	require.Equal(t, config["cancellationReasons"], read()["cancellationReasons"])
	require.Equal(t, config["loanConditions"], read()["loanConditions"])
	// Malformed replacement records must leave all previously imported data intact.
	for _, field := range []string{"unfilledReasons", "cancellationReasons", "loanConditions"} {
		original := config[field]
		for _, invalid := range []any{nil, []any{map[string]any{"code": "MissingDescription"}}} {
			config[field] = invalid
			data["name"] = "Invalid replacement"
			_, result = importRequest(t, []any{record}, "update", standardHeaders)
			require.EqualValues(t, 1, result.Entries.Failed)
			require.NotEmpty(t, result.Errors)
			require.Equal(t, original, read()[field])
			var storedName string
			require.NoError(t, dbpool.QueryRow(context.Background(), "SELECT name FROM entries WHERE id=$1", importEntryUUID(consortium)).Scan(&storedName))
			require.Equal(t, "Consortium", storedName)
		}
		config[field] = original
	}
	config["availableCopyrightTypes"] = []any{map[string]any{"code": "UnsupportedImport", "defaultDesc": "Not importable"}}
	_, rejected := importRequest(t, []any{record}, "update", standardHeaders)
	require.EqualValues(t, 1, rejected.Entries.Failed)
	require.NotEmpty(t, rejected.Errors)
	require.Equal(t, config["unfilledReasons"], read()["unfilledReasons"])
	delete(config, "availableCopyrightTypes")
	// Existing authorization applies to records carrying reason lists and loan conditions.
	restricted := map[string]string{auth.FolioPermissionsHeader: `["directory.institution.all"]`}
	response, _ := importRequest(t, []any{record}, "update", restricted)
	require.Equal(t, http.StatusUnauthorized, response.StatusCode)
	require.Equal(t, config["loanConditions"], read()["loanConditions"])
	require.Equal(t, config["unfilledReasons"], read()["unfilledReasons"])
	response, body := jsonReq(t, http.MethodPatch, path, `{"illConfig":{"availableCopyrightTypes":[{"code":"CustomCopyright","defaultDesc":"Configured through API"}]}}`, standardHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.NotEmpty(t, read()["availableCopyrightTypes"])
	data["name"] = "Consortium"
	config["unfilledReasons"] = []any{}
	config["cancellationReasons"] = []any{}
	config["loanConditions"] = []any{}
	_, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Empty(t, result.Errors)
	require.Equal(t, []any{}, read()["unfilledReasons"])
	require.Equal(t, []any{}, read()["cancellationReasons"])
	require.Equal(t, []any{}, read()["loanConditions"])
	require.NotContains(t, read(), "availableCopyrightTypes", "import replaces the whole ILL configuration")
	// Older complete configuration records remain valid and replace the lists with unset values.
	delete(config, "unfilledReasons")
	delete(config, "cancellationReasons")
	delete(config, "loanConditions")
	_, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Empty(t, result.Errors)
	current := read()
	require.NotContains(t, current, "unfilledReasons")
	require.NotContains(t, current, "cancellationReasons")
	require.NotContains(t, current, "loanConditions")
	var name string
	require.NoError(t, dbpool.QueryRow(context.Background(), "SELECT name FROM entries WHERE id=$1", importEntryUUID(consortium)).Scan(&name))
	require.Equal(t, "Consortium", name)
}
