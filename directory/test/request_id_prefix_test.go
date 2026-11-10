package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var longestRequestIdPrefix = strings.Repeat("A", 32)

var invalidRequestIdPrefixes = []string{"ABC-", "A B", "A.B", "A_B", "ÅBC", " ABC", longestRequestIdPrefix + "A"}

func TestRequestIdPrefixPersistence(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	response, body := jsonReq(t, http.MethodPost, "/entries", `{"name":"Prefix","type":"Institution","parent":"00000000-0000-0000-0000-000000000004","illConfig":{"requestIdPrefix":"ABC"}}`, headers)
	require.Equal(t, http.StatusCreated, response.StatusCode, body)
	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &entry))
	path := "/entries/by-id/" + entry["id"].(string)
	read := func() any {
		t.Helper()
		response, body := jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var stored map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &stored))
		return stored["illConfig"].(map[string]any)["requestIdPrefix"]
	}
	require.Equal(t, "ABC", read())
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"useOfferedCosts":true}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, "ABC", read())
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"requestIdPrefix":"XYZ"}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, "XYZ", read())
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"requestIdPrefix":"`+longestRequestIdPrefix+`"}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, longestRequestIdPrefix, read())
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"requestIdPrefix":"aB3"}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, "aB3", read())
	for _, invalid := range invalidRequestIdPrefixes {
		illConfig := `{"illConfig":{"requestIdPrefix":"` + invalid + `"}}`
		response, body = jsonReq(t, http.MethodPatch, path, illConfig, headers)
		require.Equal(t, http.StatusBadRequest, response.StatusCode, body)
		require.Equal(t, "aB3", read())
		response, body = jsonReq(t, http.MethodPost, "/entries", `{"name":"Invalid prefix","type":"Institution","parent":"00000000-0000-0000-0000-000000000004","illConfig":{"requestIdPrefix":"`+invalid+`"}}`, headers)
		require.Equal(t, http.StatusBadRequest, response.StatusCode, body)
	}
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"requestIdPrefix":""}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, "", read())
}

func TestImportRequestIdPrefix(t *testing.T) {
	resetImportState(t)
	key := symbolObject("ISIL", "IDPREFIX")
	record := entryImportRecord(key, "Prefix", nil, "Consortium")
	config := map[string]any{
		"iso18626Url": nil, "iso18626Vendor": nil, "lendersOfLastResort": []any{},
		"includeRequestingAgencyInfo": nil, "includeSupplierInfo": nil, "includeReturnInfo": nil,
		"includeVendorNote": nil, "useOfferedCosts": nil, "noteFieldSeparator": nil,
		"supplierPatronPattern": nil, "duplicateCheckWindowHours": nil,
	}
	record["data"].(map[string]any)["illConfig"] = config
	read := func() map[string]any {
		t.Helper()
		response, body := jsonReq(t, http.MethodGet, "/entries/by-id/"+importEntryUUID(key).String(), "", standardHeaders)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var stored map[string]any
		require.NoError(t, json.Unmarshal([]byte(body), &stored))
		return stored["illConfig"].(map[string]any)
	}
	config["requestIdPrefix"] = "ABC"
	_, result := importRequest(t, []any{record}, "", standardHeaders)
	require.Empty(t, result.Errors)
	require.Equal(t, "ABC", read()["requestIdPrefix"])
	for _, invalid := range invalidRequestIdPrefixes {
		config["requestIdPrefix"] = invalid
		_, result = importRequest(t, []any{record}, "update", standardHeaders)
		require.EqualValues(t, 1, result.Entries.Failed)
		require.NotEmpty(t, result.Errors)
		require.Equal(t, "ABC", read()["requestIdPrefix"])
	}
	for _, omit := range []bool{false, true} {
		config["requestIdPrefix"] = "ABC"
		_, result = importRequest(t, []any{record}, "update", standardHeaders)
		require.Empty(t, result.Errors)
		require.Equal(t, "ABC", read()["requestIdPrefix"])
		config["requestIdPrefix"] = nil
		if omit {
			delete(config, "requestIdPrefix")
		}
		_, result = importRequest(t, []any{record}, "update", standardHeaders)
		require.Empty(t, result.Errors)
		require.NotContains(t, read(), "requestIdPrefix")
	}
}

func TestDirectoryExportRequestIdPrefix(t *testing.T) {
	source, err := os.ReadFile("../../migration/export-crosslink-directory.sql")
	require.NoError(t, err)
	section := func(start, end string) string {
		t.Helper()
		_, sql, found := strings.Cut(string(source), start)
		require.True(t, found)
		sql, _, found = strings.Cut(sql, end)
		require.True(t, found)
		return start + sql
	}
	settingsSQL := section("CREATE TEMP TABLE crosslink_tenant_settings", "-- Collect tenant-local reason")
	duplicatesSQL := "DO $$ DECLARE problem text; BEGIN " + section("    SELECT string_agg(st_key", "    SELECT service_level, minimum_cost") + " END $$;"
	projection := strings.TrimPrefix(section(") AS catalog_config ON true", ") AS ill_config ON true"), ") AS catalog_config ON true")
	query := `WITH
 crosslink_ill_lists AS (SELECT '{}'::jsonb AS item),
 crosslink_last_resort_lenders AS (SELECT '[]'::jsonb AS item),
 entry AS (SELECT $1::uuid AS entry_id, $3::uuid AS parent_id),
 crosslink_local_entry AS (SELECT $4::uuid AS entry_id),
 local_entry AS (SELECT $1::uuid AS entry_id WHERE $2::boolean),
 mpals_entry AS (SELECT NULL::uuid AS entry_id WHERE false),
 tenant_settings AS (SELECT * FROM crosslink_tenant_settings),
 service AS (SELECT 1 AS se_id, 'iso'::text AS se_type_fk, NULL::text AS se_address WHERE false),
 service_account AS (SELECT 1 AS sa_service, $1::uuid AS sa_account_holder),
 tag AS (SELECT 1 AS id, 'pickup'::text AS norm_value),
 directory_entry_tag AS (SELECT 1 AS tag_id, $1::uuid AS directory_entry_tags_id)
 SELECT ill_config.item FROM entry LEFT JOIN local_entry ON true LEFT JOIN mpals_entry ON mpals_entry.entry_id = entry.entry_id CROSS JOIN tenant_settings
 ` + projection + ") AS ill_config ON true"
	for _, tc := range []struct {
		name, setting, fallback string
		local, branch           bool
		cleanup, wantError      string
		want                    any
	}{
		{name: "local", setting: "ABC", local: true, want: "ABC"},
		{name: "trimmed", setting: "  ABC ", local: true, want: "ABC"},
		{name: "longest", setting: longestRequestIdPrefix, local: true, want: longestRequestIdPrefix},
		{name: "too long", setting: longestRequestIdPrefix + "A", local: true, wantError: "request_id_prefix must be at most 32 ASCII letters and digits: " + longestRequestIdPrefix + "A"},
		{name: "trailing delimiter", setting: "ABC-", local: true, wantError: "request_id_prefix must be at most 32 ASCII letters and digits: ABC-"},
		{name: "inner space", setting: "A B", local: true, wantError: "request_id_prefix must be at most 32 ASCII letters and digits: A B"},
		{name: "invalid default value", setting: "  ", fallback: "A.B", local: true, wantError: "request_id_prefix must be at most 32 ASCII letters and digits: A.B"},
		{name: "default value", setting: "  ", fallback: "DEF", local: true, want: "DEF"},
		{name: "direct branch", setting: "ABC", branch: true, want: "ABC"},
		{name: "unset", local: true},
		{name: "missing setting", local: true, cleanup: "DELETE FROM app_setting;"},
		{name: "non-local", setting: "ABC"},
		{name: "duplicate setting", setting: "ABC", local: true, cleanup: "INSERT INTO app_setting SELECT * FROM app_setting;", wantError: "Export settings must have at most one row per key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := dbpool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
			_, err = tx.Exec(ctx, `CREATE TEMP TABLE app_setting (st_key text, st_value text, st_default_value text) ON COMMIT DROP;
 CREATE TEMP TABLE refdata_category (rdc_id text, rdc_description text) ON COMMIT DROP;
 CREATE TEMP TABLE refdata_value (rdv_id text, rdv_owner text, rdv_value text, rdv_label text) ON COMMIT DROP;`)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "INSERT INTO app_setting VALUES ('request_id_prefix', $1, $2)", tc.setting, tc.fallback)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, tc.cleanup+duplicatesSQL+settingsSQL)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			entryID, localOwner := uuid.New(), uuid.New()
			var parent *uuid.UUID
			if tc.branch {
				parent = &localOwner
			}
			var raw []byte
			require.NoError(t, tx.QueryRow(ctx, query, entryID, tc.local, parent, localOwner).Scan(&raw))
			var config map[string]any
			require.NoError(t, json.Unmarshal(raw, &config))
			require.Equal(t, tc.want, config["requestIdPrefix"])
		})
	}
}
