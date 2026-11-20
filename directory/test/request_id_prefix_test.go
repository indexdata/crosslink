package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var longestRequestIdPrefix = strings.Repeat("A", 16)

const exportPrefixError = `request_id_prefix must be at most 16 ASCII letters, digits, ":" or "-" after removing "/", trailing punctuation and spaces: `

var invalidRequestIdPrefixes = []string{"A B", "A.B", "A_B", "ÅBC", " ABC", "A/B", "/ABC", longestRequestIdPrefix + "A"}

// validIsilRequestIdPrefixes use the ISIL punctuation that prefixes may contain.
var validIsilRequestIdPrefixes = []string{"US-ABC", "A:B", "ABC-"}

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
	for _, valid := range validIsilRequestIdPrefixes {
		response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"requestIdPrefix":"`+valid+`"}}`, headers)
		require.Equal(t, http.StatusNoContent, response.StatusCode, body)
		require.Equal(t, valid, read())
	}
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
	response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"requestIdPrefix":null}}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Nil(t, read())
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
	for _, valid := range validIsilRequestIdPrefixes {
		config["requestIdPrefix"] = valid
		_, result = importRequest(t, []any{record}, "update", standardHeaders)
		require.Empty(t, result.Errors)
		require.Equal(t, valid, read()["requestIdPrefix"])
	}
	config["requestIdPrefix"] = "ABC"
	_, result = importRequest(t, []any{record}, "update", standardHeaders)
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
 entry AS (SELECT $1::uuid AS entry_id, $3::uuid AS parent_id, $5::text AS entry_type),
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
		{name: "too long", setting: longestRequestIdPrefix + "-A", local: true, wantError: exportPrefixError + longestRequestIdPrefix + "-A"},
		{name: "trailing delimiter removed", setting: "ABC-", local: true, want: "ABC"},
		{name: "trailing delimiters removed", setting: "US-ABC-/", local: true, want: "US-ABC"},
		{name: "inner ISIL punctuation kept", setting: "US:A-B", local: true, want: "US:A-B"},
		{name: "slashes removed", setting: "/US/A:B-C/-", local: true, want: "USA:B-C"},
		{name: "only slashes", setting: "//", local: true, wantError: exportPrefixError + "//"},
		{name: "inner other punctuation", setting: "A.B", local: true, wantError: exportPrefixError + "A.B"},
		{name: "inner space", setting: "A B", local: true, wantError: exportPrefixError + "A B"},
		{name: "only punctuation", setting: "-", local: true, wantError: exportPrefixError + "-"},
		{name: "non-ASCII punctuation kept", setting: "ABC—", local: true, wantError: exportPrefixError + "ABC—"},
		{name: "invalid default value", setting: "  ", fallback: "ÅB-", local: true, wantError: exportPrefixError + "ÅB-"},
		{name: "default value", setting: "  ", fallback: "DEF", local: true, want: "DEF"},
		{name: "direct branch inherits", setting: "ABC", branch: true},
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
			entryType := "Institution"
			if tc.branch {
				parent, entryType = &localOwner, "Branch"
			}
			var raw []byte
			require.NoError(t, tx.QueryRow(ctx, query, entryID, tc.local, parent, localOwner, entryType).Scan(&raw))
			var config map[string]any
			require.NoError(t, json.Unmarshal(raw, &config))
			require.Equal(t, tc.want, config["requestIdPrefix"])
		})
	}
}

func TestRequestIdPrefixInheritance(t *testing.T) {
	tree := setupInheritanceTree(t)
	mergeConfig(t, fixtureConsortium, "illConfig", `{"requestIdPrefix":"CONS"}`)
	prefix := func(id uuid.UUID) any {
		t.Helper()
		return getEntryJSON(t, entryPath(id), systemHeaders)["illConfig"].(map[string]any)["requestIdPrefix"]
	}
	require.Nil(t, prefix(tree.institution), "a consortium prefix is not inherited")
	require.Nil(t, prefix(tree.branch))

	response, body := jsonReq(t, http.MethodPatch, entryPath(tree.institution), `{"illConfig":{"requestIdPrefix":"INST"}}`, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, "INST", prefix(tree.branch), "a branch inherits its institution's prefix")
	local := getEntryJSON(t, entryPath(tree.branch)+"?resolve-inherit=false", systemHeaders)
	require.Nil(t, local["illConfig"], "the branch stores no ILL configuration")

	response, body = jsonReq(t, http.MethodPatch, entryPath(tree.institution), `{"illConfig":{"requestIdPrefix":""}}`, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Equal(t, "", prefix(tree.branch), "an empty prefix is inherited and selects the symbol")

	response, body = jsonReq(t, http.MethodPatch, entryPath(tree.institution), `{"illConfig":{"requestIdPrefix":null}}`, consortialHeaders)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	require.Nil(t, prefix(tree.institution))
	require.Nil(t, prefix(tree.branch))
}

func TestRequestIdPrefixIsilMigrationDown(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
	_, err = tx.Exec(ctx, "CREATE SCHEMA request_id_prefix_migration_test; SET LOCAL search_path TO request_id_prefix_migration_test")
	require.NoError(t, err)
	migration := func(version int, direction string) string {
		t.Helper()
		paths, err := filepath.Glob(fmt.Sprintf("../migrations/%03d_*.%s.sql", version, direction))
		require.NoError(t, err)
		require.Len(t, paths, 1)
		sql, err := os.ReadFile(paths[0])
		require.NoError(t, err)
		return string(sql)
	}
	for version := 1; version <= 27; version++ {
		_, err = tx.Exec(ctx, migration(version, "up"))
		require.NoError(t, err, version)
	}
	id := uuid.New()
	_, err = tx.Exec(ctx, `INSERT INTO entries (id, name, type) VALUES ($1, 'Prefix migration', 'Consortium')`, id)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO ill_configs (entry, request_id_prefix) VALUES ($1, 'US-AB:C')`, id)
	require.NoError(t, err)

	_, err = tx.Exec(ctx, migration(27, "down"))
	require.NoError(t, err, "rolling back must not fail on ISIL prefixes")

	var prefix string
	require.NoError(t, tx.QueryRow(ctx, `SELECT request_id_prefix FROM ill_configs WHERE entry = $1`, id).Scan(&prefix))
	require.Equal(t, "USABC", prefix)
	_, err = tx.Exec(ctx, `UPDATE ill_configs SET request_id_prefix = 'US-ABC' WHERE entry = $1`, id)
	require.ErrorContains(t, err, "ill_configs_request_id_prefix_check", "the previous constraint is restored")
}
