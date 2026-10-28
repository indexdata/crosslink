package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDirectoryExportDefaultCopyrightType(t *testing.T) {
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
 entry AS (SELECT $1::uuid AS entry_id, $3::uuid AS parent_id),
 crosslink_local_entry AS (SELECT CASE WHEN $2::boolean THEN $1::uuid ELSE $3::uuid END AS entry_id),
 local_entry AS (SELECT $1::uuid AS entry_id WHERE $2::boolean),
 mpals_entry AS (SELECT NULL::uuid AS entry_id WHERE false),
 tenant_settings AS (SELECT * FROM crosslink_tenant_settings),
 service AS (SELECT 1 AS se_id, 'iso'::text AS se_type_fk, NULL::text AS se_address WHERE false),
 service_account AS (SELECT 1 AS sa_service, $1::uuid AS sa_account_holder),
 tag AS (SELECT 1 AS id, 'pickup'::text AS norm_value),
 directory_entry_tag AS (SELECT 1 AS tag_id, $1::uuid AS directory_entry_tags_id)
 SELECT ill_config.item FROM entry LEFT JOIN local_entry ON true LEFT JOIN mpals_entry ON mpals_entry.entry_id = entry.entry_id CROSS JOIN tenant_settings
 ` + projection + ") AS ill_config ON true"
	code := `Custom"&`
	label := `  A "quoted" & description  `
	resetImportState(t)
	consortium := symbolObject("ISIL", "CON")
	_, result := importRequest(t, []any{entryImportRecord(consortium, "Consortium", nil, "Consortium")}, "", standardHeaders)
	require.Empty(t, result.Errors)
	for i, tc := range []struct {
		name, setting, fallback string
		label                   *string
		local, branch           bool
		cleanup, wantError      string
		want                    bool
	}{
		{name: "configured", setting: code, fallback: "ignored", label: &label, local: true, want: true},
		{name: "fallback", setting: "  ", fallback: " " + code + " ", label: &label, local: true, want: true},
		{name: "unset", local: true},
		{name: "missing setting", local: true, cleanup: "DELETE FROM app_setting;"},
		{name: "null label", setting: code, local: true, want: true},
		{name: "empty label", setting: code, label: new(string), local: true, want: true},
		{name: "blank label", setting: code, local: true, cleanup: "UPDATE refdata_value SET rdv_label=E' \\t\\n';", want: true},
		{name: "non-owner", setting: code, label: &label},
		{name: "direct branch", setting: code, label: &label, branch: true},
		{name: "missing refdata", setting: code, local: true, cleanup: "DELETE FROM refdata_value WHERE rdv_owner='copyright';", wantError: "default_copyright_type must resolve"},
		{name: "duplicate refdata", setting: code, local: true, cleanup: "INSERT INTO refdata_value SELECT 'duplicate', rdv_owner, rdv_value, rdv_label FROM refdata_value WHERE rdv_owner='copyright';", wantError: "default_copyright_type must resolve"},
		{name: "duplicate setting", setting: code, local: true, cleanup: "INSERT INTO app_setting SELECT * FROM app_setting;", wantError: "Export settings must have at most one row per key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := dbpool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
			_, err = tx.Exec(ctx, `CREATE TEMP TABLE app_setting (st_key text, st_value text, st_default_value text) ON COMMIT DROP;
 CREATE TEMP TABLE refdata_category (rdc_id text, rdc_description text) ON COMMIT DROP;
 CREATE TEMP TABLE refdata_value (rdv_id text, rdv_owner text, rdv_value text, rdv_label text) ON COMMIT DROP;
 INSERT INTO refdata_category VALUES ('copyright', 'copyrightType'), ('other', 'other');`)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "INSERT INTO app_setting VALUES ('default_copyright_type', $1, $2)", tc.setting, tc.fallback)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "INSERT INTO refdata_value VALUES ('1', 'copyright', $1, $2), ('2', 'other', $1, 'Wrong category')", code, tc.label)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, tc.cleanup+duplicatesSQL+settingsSQL)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			var parent *uuid.UUID
			if tc.branch {
				id := uuid.New()
				parent = &id
			}
			var raw []byte
			require.NoError(t, tx.QueryRow(ctx, query, uuid.New(), tc.local, parent).Scan(&raw))
			var config map[string]any
			require.NoError(t, json.Unmarshal(raw, &config))
			var want map[string]any
			if tc.want {
				description := code
				if tc.label != nil && *tc.label != "" {
					description = *tc.label
				}
				want = map[string]any{"code": code, "defaultDesc": description}
				require.Equal(t, want, config["defaultCopyrightType"])
			} else {
				require.NotContains(t, config, "defaultCopyrightType")
			}
			key := symbolObject("ISIL", fmt.Sprintf("COPYEXPORT%d", i))
			record := entryImportRecord(key, tc.name, consortium, "Institution")
			record["data"].(map[string]any)["illConfig"] = config
			for _, policy := range []string{"fail", "update"} {
				response, result := importRequest(t, []any{record}, policy, standardHeaders)
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Empty(t, result.Errors)
				response, body := jsonReq(t, http.MethodGet, "/entries/by-id/"+importEntryUUID(key).String(), "", standardHeaders)
				require.Equal(t, http.StatusOK, response.StatusCode, body)
				var entry map[string]any
				require.NoError(t, json.Unmarshal([]byte(body), &entry))
				stored := entry["illConfig"].(map[string]any)
				if tc.want {
					require.Equal(t, want, stored["defaultCopyrightType"])
				} else {
					require.NotContains(t, stored, "defaultCopyrightType")
				}
			}
		})
	}
}
