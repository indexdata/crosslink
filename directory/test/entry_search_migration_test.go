package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEntrySearchMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := tx.Exec(ctx, query, args...)
		require.NoError(t, err)
	}
	readMigration := func(version int, direction string) string {
		t.Helper()
		paths, err := filepath.Glob(fmt.Sprintf("../migrations/%03d_*.%s.sql", version, direction))
		require.NoError(t, err)
		require.Len(t, paths, 1)
		data, err := os.ReadFile(paths[0])
		require.NoError(t, err)
		return string(data)
	}
	exec("CREATE SCHEMA entry_search_migration_test; SET LOCAL search_path TO entry_search_migration_test")
	for version := 1; version <= 17; version++ {
		exec(readMigration(version, "up"))
	}
	id, nullID := uuid.New(), uuid.New()
	exec(`INSERT INTO entries (id, name, type, description, organization_id, email, phone_number)
		VALUES ($1, 'MigrationName', 'Institution', 'MigrationDescription', 'MigrationOrg', 'migration@example.test', '5551112222'),
		($2, 'NullableName', 'Institution', NULL, NULL, NULL, NULL)`, id, nullID)
	exec(`INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'MIGRATIONAUTH', 'MIGRATIONSYMBOL')`, id)
	for range 2 {
		exec(readMigration(18, "up"))
		var matches bool
		require.NoError(t, tx.QueryRow(ctx, `SELECT search @@ plainto_tsquery('simple',
			'MigrationName MigrationDescription MigrationOrg migration@example.test 5551112222 MIGRATIONAUTH:MIGRATIONSYMBOL MIGRATIONSYMBOL')
			FROM entries WHERE id=$1`, id).Scan(&matches))
		require.True(t, matches, "migration must backfill every search field and symbol")
		require.NoError(t, tx.QueryRow(ctx, `SELECT search @@ to_tsquery('simple', 'nullablename') FROM entries WHERE id=$1`, nullID).Scan(&matches))
		require.True(t, matches)
		var indexDef string
		require.NoError(t, tx.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname='entry_search_migration_test' AND indexname='entries_search_idx'`).Scan(&indexDef))
		require.Contains(t, indexDef, "USING gin (search)")
		require.NoError(t, tx.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE schemaname='entry_search_migration_test' AND indexname='symbols_owner_authority_symbol_idx'`).Scan(&indexDef))
		require.Contains(t, indexDef, "USING btree (owner, authority, symbol)")

		// Verify the triggers belong to this schema and remain usable after reapply.
		exec(`UPDATE entries SET description='UpdatedMigration' WHERE id=$1`, nullID)
		exec(`INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'REAPPLY', 'MIGRATION') ON CONFLICT DO NOTHING`, nullID)
		require.NoError(t, tx.QueryRow(ctx, `SELECT search @@ plainto_tsquery('simple', 'updatedmigration reapply:migration') FROM entries WHERE id=$1`, nullID).Scan(&matches))
		require.True(t, matches)
		exec(readMigration(18, "down"))
		var columns int
		require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns WHERE table_schema='entry_search_migration_test' AND table_name='entries' AND column_name='search'`).Scan(&columns))
		require.Zero(t, columns)
		var indexes int
		require.NoError(t, tx.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname='entry_search_migration_test' AND indexname='symbols_owner_authority_symbol_idx'`).Scan(&indexes))
		require.Zero(t, indexes)
		exec(`UPDATE entries SET description=NULL WHERE id=$1`, nullID)
		exec(`DELETE FROM symbols WHERE owner=$1`, nullID)
	}
}
