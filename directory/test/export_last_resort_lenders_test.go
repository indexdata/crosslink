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

func TestDirectoryExportLastResortLenders(t *testing.T) {
	// Run the exporter's settings, lender resolution, and ILL projection against
	// minimal legacy data, then import the output to check the stored lenders.
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
	lendersSQL := strings.ReplaceAll(
		section("CREATE TEMP TABLE crosslink_last_resort_lender_symbols", "CREATE TEMP TABLE crosslink_mpals_entry"),
		":'mode'", "'tenant'")
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

	resetImportState(t)
	consortium := symbolObject("ISIL", "CON")
	lender := symbolObject("ISIL", "US-LENDER")
	second := symbolObject("ISIL", "US-SECOND")
	_, result := importRequest(t, []any{
		entryImportRecord(consortium, "Consortium", nil, "Consortium"),
		entryImportRecord(lender, "Lender", consortium, "Institution"),
		entryImportRecord(second, "Second lender", consortium, "Institution"),
	}, "", standardHeaders)
	require.Empty(t, result.Errors)

	for i, tc := range []struct {
		name, setting, fallback string
		local, branch           bool
		cleanup, wantError      string
		want                    []map[string]any
	}{
		{name: "configured", setting: "ISIL:US-LENDER", local: true, want: []map[string]any{lender}},
		{name: "list", setting: " isil:us-second , ISIL:US-LENDER,, ISIL:US-SECOND ", local: true, want: []map[string]any{second, lender}},
		{name: "fallback", setting: "  ", fallback: " ISIL:US-LENDER ", local: true, want: []map[string]any{lender}},
		{name: "unset", local: true},
		{name: "missing setting", local: true, cleanup: "DELETE FROM app_setting;"},
		{name: "non-owner", setting: "ISIL:US-LENDER"},
		{name: "direct branch", setting: "ISIL:US-LENDER", branch: true},
		{name: "unknown symbol", setting: "ISIL:US-MISSING", local: true, wantError: "last_resort_lenders must list"},
		{name: "missing authority", setting: "US-LENDER", local: true, wantError: "last_resort_lenders must list"},
		{name: "ambiguous symbol", setting: "ISIL:US-LENDER", local: true, cleanup: "INSERT INTO crosslink_symbols VALUES (gen_random_uuid(), 'ISIL', 'US-LENDER');", wantError: "last_resort_lenders must list"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := dbpool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
			_, err = tx.Exec(ctx, `CREATE TEMP TABLE app_setting (st_key text, st_value text, st_default_value text) ON COMMIT DROP;
 CREATE TEMP TABLE refdata_category (rdc_id text, rdc_description text) ON COMMIT DROP;
 CREATE TEMP TABLE refdata_value (rdv_id text, rdv_owner text, rdv_value text, rdv_label text) ON COMMIT DROP;
 CREATE TEMP TABLE crosslink_symbols (entry_id uuid, authority text, symbol text) ON COMMIT DROP;`)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "INSERT INTO app_setting VALUES ('last_resort_lenders', $1, $2)", tc.setting, tc.fallback)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "INSERT INTO crosslink_symbols VALUES ($1, 'ISIL', 'US-LENDER'), ($2, 'ISIL', 'US-SECOND')",
				importEntryUUID(lender), importEntryUUID(second))
			require.NoError(t, err)
			_, err = tx.Exec(ctx, tc.cleanup+settingsSQL+lendersSQL)
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
			wantIDs := []any{}
			wantStored := []any{}
			for _, symbol := range tc.want {
				wantIDs = append(wantIDs, importEntryUUID(symbol).String())
				wantStored = append(wantStored, symbol)
			}
			require.Equal(t, wantIDs, config["lendersOfLastResort"])

			key := symbolObject("ISIL", fmt.Sprintf("LOREXPORT%d", i))
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
				stored, _ := entry["illConfig"].(map[string]any)["lendersOfLastResort"].([]any)
				if len(wantStored) == 0 {
					require.Empty(t, stored)
				} else {
					require.Equal(t, wantStored, stored)
				}
			}
		})
	}
}
