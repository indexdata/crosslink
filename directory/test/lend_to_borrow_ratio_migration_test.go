package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestLendToBorrowRatioBoundsMigration(t *testing.T) {
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
	exec("CREATE SCHEMA ratio_bounds_migration_test; SET LOCAL search_path TO ratio_bounds_migration_test")
	for version := 1; version <= 14; version++ {
		exec(readMigration(version, "up"))
	}
	id := uuid.New()
	exec("INSERT INTO entries (id, name, type, lend_to_borrow_ratio) VALUES ($1, 'Ratio migration test', 'Institution', '10000:1')", id)

	// Existing out-of-range values must be corrected, rather than silently altered.
	exec("SAVEPOINT before_bounds")
	_, err = tx.Exec(ctx, readMigration(15, "up"))
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "23514", pgErr.Code)
	exec("ROLLBACK TO SAVEPOINT before_bounds")
	exec("UPDATE entries SET lend_to_borrow_ratio = '9999.99:9999.99' WHERE id = $1", id)
	exec(readMigration(15, "up"))
	var ratio string
	require.NoError(t, tx.QueryRow(ctx, "SELECT lend_to_borrow_ratio FROM entries WHERE id = $1", id).Scan(&ratio))
	require.Equal(t, "9999.99:9999.99", ratio)

	for _, invalid := range []string{"10000:1", "1:10000", "1.001:1", "1:1.001", "00001:1", "1:00001"} {
		exec("SAVEPOINT before_invalid")
		_, err = tx.Exec(ctx, "UPDATE entries SET lend_to_borrow_ratio = $1 WHERE id = $2", invalid, id)
		require.ErrorAs(t, err, &pgErr)
		require.Equal(t, "23514", pgErr.Code)
		require.Equal(t, "entries_lend_to_borrow_ratio_bounds_check", pgErr.ConstraintName)
		exec("ROLLBACK TO SAVEPOINT before_invalid")
	}
	exec("UPDATE entries SET lend_to_borrow_ratio = NULL WHERE id = $1", id)
	exec(readMigration(15, "down"))
	exec("UPDATE entries SET lend_to_borrow_ratio = '10000:1' WHERE id = $1", id)
	exec("UPDATE entries SET lend_to_borrow_ratio = '1:0.001' WHERE id = $1", id)
}
