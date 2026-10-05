package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDirectoryExportLendToBorrowRatio(t *testing.T) {
	// Execute the exporter's actual mapping, validation, ordering, and ratio
	// projection against minimal legacy tables, then import the exported field.
	source, err := os.ReadFile("../../migration/export-crosslink-directory.sql")
	require.NoError(t, err)
	_, ratioSQL, found := strings.Cut(string(source), "CREATE TEMP TABLE crosslink_entry_ratios")
	require.True(t, found)
	ratioSQL, _, found = strings.Cut(ratioSQL, "CREATE TEMP TABLE crosslink_symbols")
	require.True(t, found)
	ratioSQL = "CREATE TEMP TABLE crosslink_entry_ratios" + ratioSQL
	_, orderedSQL, found := strings.Cut(string(source), "WITH ordered_entries AS (")
	require.True(t, found)
	orderedSQL, _, found = strings.Cut(orderedSQL, "),\nentry_records AS (")
	require.True(t, found)
	_, projection, found := strings.Cut(string(source), "'lendToBorrowRatio', ")
	require.True(t, found)
	projection, _, found = strings.Cut(projection, ",\n")
	require.True(t, found)
	query := "WITH ordered_entries AS (" + orderedSQL + ") SELECT entry_id, jsonb_build_object('lendToBorrowRatio', " + projection + ") FROM ordered_entries AS entry ORDER BY entry_id"

	resetImportState(t)
	for i, tc := range []struct {
		name        string
		values      []string
		missingText bool
		wantError   string
	}{
		{name: "absent property"},
		{name: "ordinary ratio", values: []string{"2:1"}},
		{name: "decimal ratio", values: []string{"0.5:2"}},
		{name: "leading zeros", values: []string{"05.50:1"}},
		{name: "maximum components", values: []string{"9999.99:9999.99"}},
		{name: "minimum loans", values: []string{"0.01:9999.99"}},
		{name: "minimum borrows", values: []string{"9999.99:0.01"}},
		{name: "maximum length", values: []string{"0000.01:0001.00"}},
		{name: "blank", values: []string{""}, wantError: "invalid"},
		{name: "whitespace", values: []string{" 2:1 "}, wantError: "invalid"},
		{name: "newline", values: []string{"2:1\n"}, wantError: "invalid"},
		{name: "zero loans", values: []string{"0:1"}, wantError: "invalid"},
		{name: "zero borrows", values: []string{"1:0"}, wantError: "invalid"},
		{name: "negative", values: []string{"-1:2"}, wantError: "invalid"},
		{name: "malformed", values: []string{"nonsense"}, wantError: "invalid"},
		{name: "too many integer digits", values: []string{"10000:1"}, wantError: "invalid"},
		{name: "too many decimal places", values: []string{"1:1.001"}, wantError: "invalid"},
		{name: "overlong", values: []string{"00000.01:0001.00"}, wantError: "invalid"},
		{name: "missing text value", values: []string{"2:1"}, missingText: true, wantError: "invalid"},
		{name: "duplicate identical values", values: []string{"2:1", "2:1"}, wantError: "multiple"},
		{name: "duplicate different values", values: []string{"2:1", "3:1"}, wantError: "multiple"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := dbpool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
			_, err = tx.Exec(ctx, `
				CREATE TEMP TABLE crosslink_entry_base (entry_id text, name text, custom_properties_id bigint) ON COMMIT DROP;
				INSERT INTO crosslink_entry_base VALUES ('institution', 'Institution', 1), ('branch', 'Branch', 2), ('consortium', 'Consortium', NULL);
				CREATE TEMP TABLE crosslink_hierarchy (entry_id text, depth integer) ON COMMIT DROP;
				INSERT INTO crosslink_hierarchy VALUES ('institution', 1), ('branch', 2), ('consortium', 0);
				CREATE TEMP TABLE crosslink_entry_keys (entry_id text, authority text, symbol text) ON COMMIT DROP;
				INSERT INTO crosslink_entry_keys VALUES ('institution', 'ISIL', 'INST'), ('branch', 'ISIL', 'BRANCH'), ('consortium', 'ISIL', 'CON');
				CREATE TEMP TABLE custom_property (id bigint, parent_id bigint, definition_id text) ON COMMIT DROP;
				CREATE TEMP TABLE custom_property_definition (pd_id text, pd_name text) ON COMMIT DROP;
				CREATE TEMP TABLE custom_property_text (id bigint, value text) ON COMMIT DROP;
				INSERT INTO custom_property_definition VALUES ('ratio', 'policy.ill.InstitutionalLoanToBorrowRatio'), ('other', 'unrelated');
				INSERT INTO custom_property VALUES (100, 2, 'ratio'), (101, 1, 'other');
				INSERT INTO custom_property_text VALUES (100, '3:2'), (101, 'invalid unrelated value');
			`)
			require.NoError(t, err)
			for j, value := range tc.values {
				_, err = tx.Exec(ctx, `INSERT INTO custom_property VALUES ($1, 1, 'ratio')`, j+1)
				require.NoError(t, err)
				if !tc.missingText {
					_, err = tx.Exec(ctx, `INSERT INTO custom_property_text VALUES ($1, $2)`, j+1, value)
					require.NoError(t, err)
				}
			}
			_, err = tx.Exec(ctx, ratioSQL)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError+" policy.ill.InstitutionalLoanToBorrowRatio")
				require.ErrorContains(t, err, "institution (Institution)")
				return
			}
			require.NoError(t, err)
			rows, err := tx.Query(ctx, query)
			require.NoError(t, err)
			defer rows.Close()
			exported := make(map[string]map[string]any)
			for rows.Next() {
				var id string
				var raw []byte
				require.NoError(t, rows.Scan(&id, &raw))
				var data map[string]any
				require.NoError(t, json.Unmarshal(raw, &data))
				exported[id] = data
			}
			require.NoError(t, rows.Err())
			require.Len(t, exported, 3)
			require.Equal(t, "3:2", exported["branch"]["lendToBorrowRatio"])
			require.Contains(t, exported["consortium"], "lendToBorrowRatio")
			require.Nil(t, exported["consortium"]["lendToBorrowRatio"])
			var want *string
			if len(tc.values) == 1 {
				want = &tc.values[0]
				require.Equal(t, *want, exported["institution"]["lendToBorrowRatio"])
			} else {
				require.Contains(t, exported["institution"], "lendToBorrowRatio")
				require.Nil(t, exported["institution"]["lendToBorrowRatio"])
			}

			key := symbolObject("ISIL", fmt.Sprintf("EXPORTEDRATIO%d", i))
			record := entryImportRecord(key, tc.name, nil, "Institution")
			data := record["data"].(map[string]any)
			// Verify creation, then seed a different ratio and restore the export
			// using update, including clearing the seeded value with null.
			seed := "1:1"
			for _, step := range []struct {
				policy string
				value  *string
			}{
				{"fail", want}, {"update", &seed}, {"update", want},
			} {
				data["lendToBorrowRatio"] = step.value
				response, result := importRequest(t, []any{record}, step.policy, standardHeaders)
				require.Equal(t, http.StatusOK, response.StatusCode)
				require.Empty(t, result.Errors)
				id := importedEntryID(t, "ISIL", key["symbol"].(string))
				var persisted *string
				require.NoError(t, dbpool.QueryRow(ctx, `SELECT lend_to_borrow_ratio FROM entries WHERE id=$1`, id).Scan(&persisted))
				require.Equal(t, step.value, persisted)
			}
		})
	}
}
