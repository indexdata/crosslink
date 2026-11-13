package test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hridProblem = " must be ASCII letters and digits, optionally followed by a hyphen and digits"

func TestExportPreflightRejectsInvalidHrid(t *testing.T) {
	// Execute the actual preflight check without requiring the full legacy schema.
	source, err := os.ReadFile("../../../migration/export-crosslink-open-patron-requests.sql")
	require.NoError(t, err)
	const start = "SELECT 'request ' || pr_id || ': HRID '"
	_, check, found := strings.Cut(string(source), start)
	require.True(t, found, "HRID preflight check must exist")
	check, _, found = strings.Cut(check, ") AS validation;")
	require.True(t, found, "HRID preflight check end must exist")
	query := start + check

	for _, tc := range []struct {
		name string
		side string
		hrid pgtype.Text
		want string
	}{
		{name: "alphanumeric", side: "borrowing", hrid: pgtype.Text{String: "ABC123", Valid: true}},
		{name: "lowercase", side: "borrowing", hrid: pgtype.Text{String: "abc123", Valid: true}},
		{name: "digits only", side: "borrowing", hrid: pgtype.Text{String: "123", Valid: true}},
		{name: "missing HRID keeps UUID", side: "borrowing"},
		{name: "lending is not checked", side: "lending", hrid: pgtype.Text{String: "AB 1", Valid: true}},
		{name: "hyphen before number", side: "borrowing", hrid: pgtype.Text{String: "ABC-10008", Valid: true}},
		{name: "hyphen before letters", side: "borrowing", hrid: pgtype.Text{String: "ABC-X1", Valid: true},
			want: "request pr1: HRID ABC-X1" + hridProblem},
		{name: "two hyphens", side: "borrowing", hrid: pgtype.Text{String: "A-B-1", Valid: true},
			want: "request pr1: HRID A-B-1" + hridProblem},
		{name: "space", side: "borrowing", hrid: pgtype.Text{String: "AB 1", Valid: true},
			want: "request pr1: HRID AB 1" + hridProblem},
		{name: "non-ASCII", side: "borrowing", hrid: pgtype.Text{String: "ÅB1", Valid: true},
			want: "request pr1: HRID ÅB1" + hridProblem},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			tx, err := importTestPool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
			_, err = tx.Exec(ctx, `CREATE TEMP TABLE crosslink_open_requests (
				pr_id text, pr_hrid text, import_side text
			) ON COMMIT DROP`)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO crosslink_open_requests VALUES ('pr1', $1, $2)`, tc.hrid, tc.side)
			require.NoError(t, err)
			rows, err := tx.Query(ctx, query)
			require.NoError(t, err)
			defer rows.Close()
			var problems []string
			for rows.Next() {
				var problem string
				require.NoError(t, rows.Scan(&problem))
				problems = append(problems, problem)
			}
			require.NoError(t, rows.Err())
			if tc.want == "" {
				assert.Empty(t, problems)
			} else {
				assert.Equal(t, []string{tc.want}, problems)
			}
		})
	}
}
