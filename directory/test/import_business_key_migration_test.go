package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImportBusinessKeyMigrationBackfillsLegacyNames(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, "CREATE SCHEMA import_business_key_migration_test; SET LOCAL search_path TO import_business_key_migration_test")
	require.NoError(t, err)
	apply := func(pattern string) {
		t.Helper()
		paths, globErr := filepath.Glob("../migrations/" + pattern)
		require.NoError(t, globErr)
		require.NotEmpty(t, paths)
		for _, path := range paths {
			data, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			_, execErr := tx.Exec(ctx, string(data))
			require.NoError(t, execErr, "apply %s", path)
		}
	}
	for version := 1; version <= 7; version++ {
		apply(fmt.Sprintf("%03d_*.up.sql", version))
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO entries (id, name, type) VALUES
			('00000000-0000-0000-0000-000000000001', 'Consortium', 'Consortium'),
			('00000000-0000-0000-0000-000000000002', 'Member', 'Institution');
		INSERT INTO tiers (id, consortium, name) VALUES
			('10000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', NULL),
			('10000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', E' \t'),
			('10000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'Legacy tier 10000000-0000-0000-0000-000000000001');
		INSERT INTO networks (id, consortium, name) VALUES
			('20000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', NULL),
			('20000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', E'\n '),
			('20000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'Legacy network 20000000-0000-0000-0000-000000000001');
		INSERT INTO entry_tiers (entry, tier) VALUES
			('00000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001'),
			('00000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000002'),
			('00000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000003');
		INSERT INTO entry_networks (entry, network, priority) VALUES
			('00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', 1),
			('00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', 2),
			('00000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000003', 3);`)
	require.NoError(t, err)

	apply("008_*.up.sql")

	assertLegacyName := func(table, id, want string) {
		t.Helper()
		var got string
		err := tx.QueryRow(ctx, "SELECT name FROM "+table+" WHERE id=$1", id).Scan(&got) //nolint:gosec // fixed test table names
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
	assertLegacyName("tiers", "10000000-0000-0000-0000-000000000001", "Legacy tier 10000000-0000-0000-0000-000000000001 (1)")
	assertLegacyName("tiers", "10000000-0000-0000-0000-000000000002", "Legacy tier 10000000-0000-0000-0000-000000000002")
	assertLegacyName("tiers", "10000000-0000-0000-0000-000000000003", "Legacy tier 10000000-0000-0000-0000-000000000001")
	assertLegacyName("networks", "20000000-0000-0000-0000-000000000001", "Legacy network 20000000-0000-0000-0000-000000000001 (1)")
	assertLegacyName("networks", "20000000-0000-0000-0000-000000000002", "Legacy network 20000000-0000-0000-0000-000000000002")
	assertLegacyName("networks", "20000000-0000-0000-0000-000000000003", "Legacy network 20000000-0000-0000-0000-000000000001")

	var tierAssignments, networkAssignments int
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM entry_tiers").Scan(&tierAssignments))
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM entry_networks").Scan(&networkAssignments))
	require.Equal(t, 3, tierAssignments)
	require.Equal(t, 3, networkAssignments)
}
