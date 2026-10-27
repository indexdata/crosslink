package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDirectoryExportIllConfigLists(t *testing.T) {
	source, err := os.ReadFile("../../migration/export-crosslink-directory.sql")
	require.NoError(t, err)
	_, listsSQL, found := strings.Cut(string(source), "CREATE TEMP TABLE crosslink_ill_lists")
	require.True(t, found)
	listsSQL, _, found = strings.Cut(listsSQL, "CREATE TEMP TABLE crosslink_local_entry")
	require.True(t, found)
	listsSQL = "CREATE TEMP TABLE crosslink_ill_lists" + listsSQL
	_, projection, found := strings.Cut(string(source), ") AS catalog_config ON true")
	require.True(t, found)
	projection, _, found = strings.Cut(projection, ") AS ill_config ON true")
	require.True(t, found)
	query := `WITH
 entry AS (SELECT $1::uuid AS entry_id, NULL::uuid AS parent_id),
 crosslink_local_entry AS (SELECT $1::uuid AS entry_id WHERE $2::boolean),
 local_entry AS (SELECT $1::uuid AS entry_id WHERE $2::boolean),
 mpals_entry AS (SELECT NULL::uuid AS entry_id WHERE false),
 tenant_settings AS (SELECT NULL::text AS max_requests_per_patron, NULL::text AS duplicate_check_window_hours),
 service AS (SELECT 1 AS se_id, 'iso'::text AS se_type_fk, NULL::text AS se_address WHERE false),
 service_account AS (SELECT 1 AS sa_service, $1::uuid AS sa_account_holder),
 tag AS (SELECT 1 AS id, 'pickup'::text AS norm_value),
 directory_entry_tag AS (SELECT 1 AS tag_id, $1::uuid AS directory_entry_tags_id)
 SELECT ill_config.item FROM entry LEFT JOIN local_entry ON true LEFT JOIN mpals_entry ON mpals_entry.entry_id = entry.entry_id CROSS JOIN tenant_settings
 ` + projection + ") AS ill_config ON true"

	// These rows mirror mod-rs RefdataValueData, with additional fallback and
	// escaping cases. IDs deliberately differ from the expected label ordering.
	fixture := `INSERT INTO refdata_category VALUES
 ('cancel', 'cancellationReasons'), ('unfilled', 'cannotSupplyReasons'),
 ('loan', 'loanConditions'), ('other', 'unrelated');
 INSERT INTO refdata_value VALUES
 ('1', 'cancel', 'patron_requested', 'User requested cancellation'),
 ('2', 'cancel', 'available_locally', 'Requested item is locally available'),
 ('3', 'unfilled', 'unavailable', 'No longer available'),
 ('4', 'unfilled', 'empty', ''),
 ('5', 'unfilled', 'null', NULL),
 ('6', 'unfilled', 'whitespace', E' \t\n'),
 ('7', 'loan', 'LibraryUseOnly', 'LibraryUseOnly'),
 ('8', 'loan', 'NoReproduction', NULL),
 ('9', 'loan', 'empty', ''),
 ('10', 'loan', 'whitespace', E' \t\n'),
 ('11', 'loan', 'Custom"&', '  A "quoted" & condition  '),
 ('12', 'other', 'ignored', 'Ignored');`
	list := func(pairs ...string) []any {
		items := make([]any, 0, len(pairs)/2)
		for i := 0; i < len(pairs); i += 2 {
			items = append(items, map[string]any{"code": pairs[i], "defaultDesc": pairs[i+1]})
		}
		return items
	}
	expected := map[string]any{
		"cancellationReasons": list("available_locally", "Requested item is locally available", "patron_requested", "User requested cancellation"),
		"unfilledReasons":     list("empty", "empty", "whitespace", "whitespace", "unavailable", "No longer available", "null", "null"),
		"loanConditions":      list("empty", "empty", "whitespace", "whitespace", "Custom\"&", "  A \"quoted\" & condition  ", "LibraryUseOnly", "LibraryUseOnly", "NoReproduction", "NoReproduction"),
	}
	resetImportState(t)
	for i, tc := range []struct {
		name    string
		cleanup string
		local   bool
		fields  []string
	}{
		{"all vocabularies", "", true, []string{"cancellationReasons", "unfilledReasons", "loanConditions"}},
		{"non-owner pickup", "", false, nil},
		{"missing vocabularies", "DELETE FROM refdata_value; DELETE FROM refdata_category;", true, nil},
		{"empty vocabularies", "DELETE FROM refdata_value;", true, nil},
		{"independent omission", "DELETE FROM refdata_value WHERE rdv_owner IN ('cancel', 'loan'); DELETE FROM refdata_category WHERE rdc_id='cancel';", true, []string{"unfilledReasons"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := dbpool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
			_, err = tx.Exec(ctx, `CREATE TEMP TABLE refdata_category (rdc_id text, rdc_description text) ON COMMIT DROP;
    CREATE TEMP TABLE refdata_value (rdv_id text, rdv_owner text, rdv_value text, rdv_label text) ON COMMIT DROP;`+fixture+tc.cleanup+listsSQL)
			require.NoError(t, err)
			var raw []byte
			require.NoError(t, tx.QueryRow(ctx, query, uuid.New(), tc.local).Scan(&raw))
			var config map[string]any
			require.NoError(t, json.Unmarshal(raw, &config))
			require.NotContains(t, config, "cannotSupplyReasons")
			for field, want := range expected {
				if slices.Contains(tc.fields, field) {
					require.Equal(t, want, config[field])
				} else {
					require.NotContains(t, config, field)
				}
			}
			// Re-running the projection must preserve array ordering.
			var repeated []byte
			require.NoError(t, tx.QueryRow(ctx, query, uuid.New(), tc.local).Scan(&repeated))
			require.JSONEq(t, string(raw), string(repeated))

			key := symbolObject("ISIL", fmt.Sprintf("EXPORTEDLIST%d", i))
			record := entryImportRecord(key, tc.name, nil, "Institution")
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
				for field := range expected {
					if slices.Contains(tc.fields, field) {
						require.Equal(t, config[field], stored[field])
					} else {
						require.NotContains(t, stored, field)
					}
				}
			}
		})
	}
}
