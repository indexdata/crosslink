package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunMigrateScriptsReturnsDuplicateVersionError(t *testing.T) {
	originalFolder, originalConnection := MigrationsFolder, ConnectionString
	t.Cleanup(func() { MigrationsFolder, ConnectionString = originalFolder, originalConnection })
	directory := t.TempDir()
	for _, name := range []string{"014_catalog_query_year.up.sql", "014_lend_to_borrow_ratio.up.sql"} {
		require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte("SELECT 1;"), 0600))
	}
	MigrationsFolder = "file://" + directory
	ConnectionString = "postgres://unused:unused@127.0.0.1:1/unused?sslmode=disable"
	err := RunMigrateScripts()
	require.ErrorContains(t, err, "initialize Directory migrations")
	require.ErrorContains(t, err, "duplicate migration file")
	require.Error(t, errors.Unwrap(err))
}
