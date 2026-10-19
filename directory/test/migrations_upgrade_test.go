package test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/app"
	"github.com/stretchr/testify/require"
)

func TestMigrationUpgradeFromMainVersion14(t *testing.T) {
	ctx := context.Background()
	// A separate schema lets the real migration runner exercise version tracking
	// without changing the integration suite's fully migrated tables.
	_, err := dbpool.Exec(ctx, "CREATE SCHEMA migration_upgrade_test")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := dbpool.Exec(ctx, "DROP SCHEMA migration_upgrade_test CASCADE")
		require.NoError(t, err)
	})
	originalFolder, originalConnection := app.MigrationsFolder, app.ConnectionString
	t.Cleanup(func() { app.MigrationsFolder, app.ConnectionString = originalFolder, originalConnection })
	connectionURL, err := url.Parse(originalConnection)
	require.NoError(t, err)
	query := connectionURL.Query()
	query.Set("search_path", "migration_upgrade_test,public")
	connectionURL.RawQuery = query.Encode()
	app.ConnectionString = connectionURL.String()

	migrationDirectory := t.TempDir()
	copyVersion := func(version int) {
		t.Helper()
		for _, direction := range []string{"up", "down"} {
			paths, err := filepath.Glob(fmt.Sprintf("../migrations/%03d_*.%s.sql", version, direction))
			require.NoError(t, err)
			require.Len(t, paths, 1)
			data, err := os.ReadFile(paths[0])
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(migrationDirectory, filepath.Base(paths[0])), data, 0600))
		}
	}
	for version := 1; version <= 14; version++ {
		copyVersion(version)
	}
	app.MigrationsFolder = "file://" + migrationDirectory
	require.NoError(t, app.RunMigrateScripts())

	id := uuid.New()
	_, err = dbpool.Exec(ctx, `INSERT INTO migration_upgrade_test.entries (id, name, type) VALUES ($1, 'Existing consortium', 'Consortium')`, id)
	require.NoError(t, err)
	_, err = dbpool.Exec(ctx, `INSERT INTO migration_upgrade_test.catalog_configs (entry, query_year) VALUES ($1, 'existing-year-query')`, id)
	require.NoError(t, err)
	_, err = dbpool.Exec(ctx, `INSERT INTO migration_upgrade_test.ill_configs (entry) VALUES ($1)`, id)
	require.NoError(t, err)

	for version := 15; version <= 17; version++ {
		copyVersion(version)
	}
	require.NoError(t, app.RunMigrateScripts())
	require.NoError(t, app.RunMigrateScripts(), "an up-to-date schema is successful")
	var version int
	var dirty bool
	require.NoError(t, dbpool.QueryRow(ctx, `SELECT version, dirty FROM migration_upgrade_test.schema_migrations`).Scan(&version, &dirty))
	require.Equal(t, 17, version)
	require.False(t, dirty)
	var name, yearQuery string
	var ratio, policy *string
	require.NoError(t, dbpool.QueryRow(ctx, `SELECT e.name, c.query_year, e.lend_to_borrow_ratio, i.load_balancing_policy FROM migration_upgrade_test.entries e JOIN migration_upgrade_test.catalog_configs c ON c.entry=e.id JOIN migration_upgrade_test.ill_configs i ON i.entry=e.id WHERE e.id=$1`, id).Scan(&name, &yearQuery, &ratio, &policy))
	require.Equal(t, "Existing consortium", name)
	require.Equal(t, "existing-year-query", yearQuery)
	require.Nil(t, ratio)
	require.Nil(t, policy)

	// Failed SQL must reach the caller rather than continuing against an incomplete schema.
	require.NoError(t, os.WriteFile(filepath.Join(migrationDirectory, "018_invalid.up.sql"), []byte("SELECT * FROM migration_upgrade_test.nonexistent_table;"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(migrationDirectory, "018_invalid.down.sql"), []byte("SELECT 1;"), 0600))
	err = app.RunMigrateScripts()
	require.ErrorContains(t, err, "apply Directory migrations")
	require.ErrorContains(t, err, "nonexistent_table")
}
