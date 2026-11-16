package test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultCopyrightTypePersistence(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	response, body := jsonReq(t, http.MethodPost, "/entries", `{"name":"Copyright default","type":"Institution","parent":"00000000-0000-0000-0000-000000000004","illConfig":{"defaultCopyrightType":{"code":"Custom","defaultDesc":"Custom description"},"availableCopyrightTypes":[]}}`, headers)
	require.Equal(t, http.StatusCreated, response.StatusCode, body)
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &entry))
	path := "/entries/by-id/" + entry["id"].(string)
	read := func() map[string]any {
		t.Helper()
		response, body := jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var stored map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &stored))
		return stored["illConfig"].(map[string]any)
	}
	original := map[string]any{"code": "Custom", "defaultDesc": "Custom description"}
	require.Equal(t, original, read()["defaultCopyrightType"])
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"useOfferedCosts":true}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, original, read()["defaultCopyrightType"])
	for _, invalid := range []string{`"Custom"`, `[]`, `{}`, `{"code":"A"}`, `{"defaultDesc":"B"}`, `{"code":1,"defaultDesc":"B"}`, `{"code":"A","defaultDesc":"B","extra":true}`} {
		response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"defaultCopyrightType":`+invalid+`}}`, headers)
		require.Equal(t, http.StatusBadRequest, response.StatusCode, body)
		require.Equal(t, original, read()["defaultCopyrightType"])
	}
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"defaultCopyrightType":{"code":"Replacement","defaultDesc":"Quoted \" & description"}}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, map[string]any{"code": "Replacement", "defaultDesc": "Quoted \" & description"}, read()["defaultCopyrightType"])
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"defaultCopyrightType":null}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	config := read()
	require.NotContains(t, config, "defaultCopyrightType")
	require.Equal(t, true, config["useOfferedCosts"])
	require.Equal(t, []any{}, config["availableCopyrightTypes"])
	var cleared bool
	require.NoError(t, dbpool.QueryRow(context.Background(), "SELECT default_copyright_type IS NULL FROM ill_configs WHERE entry=$1", entry["id"]).Scan(&cleared))
	require.True(t, cleared)
}

func TestImportDefaultCopyrightType(t *testing.T) {
	resetImportState(t)
	key := symbolObject("ISIL", "COPYRIGHT")
	record := entryImportRecord(key, "Copyright", nil, "Consortium")
	data := record["data"].(map[string]any)
	// Reuse a complete legacy configuration, with the new field remaining optional.
	config := map[string]any{
		"iso18626Url": nil, "iso18626Vendor": nil, "lendersOfLastResort": []any{},
		"includeRequestingAgencyInfo": nil, "includeSupplierInfo": nil, "includeReturnInfo": nil,
		"includeVendorNote": nil, "useOfferedCosts": nil, "noteFieldSeparator": nil,
		"supplierPatronPattern": nil, "duplicateCheckWindowHours": nil,
	}
	data["illConfig"] = config
	read := func() map[string]any {
		t.Helper()
		response, body := jsonReq(t, http.MethodGet, "/entries/by-id/"+importEntryUUID(key).String(), "", standardHeaders)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var stored map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &stored))
		return stored["illConfig"].(map[string]any)
	}
	original := map[string]any{"code": "Custom", "defaultDesc": "Custom description"}
	config["defaultCopyrightType"] = original
	_, result := importRequest(t, []any{record}, "", standardHeaders)
	require.Empty(t, result.Errors)
	require.EqualValues(t, 1, result.Entries.Imported)
	require.Equal(t, original, read()["defaultCopyrightType"])
	for _, invalid := range []any{"Custom", []any{}, map[string]any{}, map[string]any{"code": "A"}, map[string]any{"code": "A", "defaultDesc": 2}, map[string]any{"code": "A", "defaultDesc": "B", "extra": true}} {
		config["defaultCopyrightType"] = invalid
		_, result = importRequest(t, []any{record}, "update", standardHeaders)
		require.EqualValues(t, 1, result.Entries.Failed)
		require.NotEmpty(t, result.Errors)
		require.Equal(t, original, read()["defaultCopyrightType"])
	}
	replacement := map[string]any{"code": "Replacement", "defaultDesc": "New description"}
	config["defaultCopyrightType"] = replacement
	_, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Empty(t, result.Errors)
	require.Equal(t, replacement, read()["defaultCopyrightType"])
	for _, omit := range []bool{false, true} {
		config["defaultCopyrightType"] = original
		_, result = importRequest(t, []any{record}, "update", standardHeaders)
		require.Empty(t, result.Errors)
		require.Equal(t, original, read()["defaultCopyrightType"])
		config["defaultCopyrightType"] = nil
		if omit {
			delete(config, "defaultCopyrightType")
		}
		_, result = importRequest(t, []any{record}, "update", standardHeaders)
		require.Empty(t, result.Errors)
		require.NotContains(t, read(), "defaultCopyrightType")
	}
}
