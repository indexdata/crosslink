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

func TestExportPreflightRejectsNonAlphanumericHrid(t *testing.T) {
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
		{name: "lending is not checked", side: "lending", hrid: pgtype.Text{String: "ABC-1", Valid: true}},
		{name: "hyphen", side: "borrowing", hrid: pgtype.Text{String: "ABC-1", Valid: true},
			want: "request pr1: HRID ABC-1 must contain only ASCII letters and digits"},
		{name: "space", side: "borrowing", hrid: pgtype.Text{String: "AB 1", Valid: true},
			want: "request pr1: HRID AB 1 must contain only ASCII letters and digits"},
		{name: "non-ASCII", side: "borrowing", hrid: pgtype.Text{String: "ÅB1", Valid: true},
			want: "request pr1: HRID ÅB1 must contain only ASCII letters and digits"},
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
