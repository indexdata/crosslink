package test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublicationMetadataMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(sql string) { t.Helper(); _, err := tx.Exec(ctx, sql); require.NoError(t, err) }
	exec("CREATE SCHEMA publication_metadata_migration_test; SET LOCAL search_path TO publication_metadata_migration_test")
	exec("CREATE TABLE catalog_configs (metadata_marc21_title text); INSERT INTO catalog_configs VALUES ('245$a')")
	up, err := os.ReadFile("../migrations/023_catalog_publication_metadata.up.sql")
	require.NoError(t, err)
	exec(string(up))
	var title string
	var publisher, date, place *string
	require.NoError(t, tx.QueryRow(ctx, "SELECT metadata_marc21_title, metadata_marc21_publisher, metadata_marc21_publication_date, metadata_marc21_place_of_publication FROM catalog_configs").Scan(&title, &publisher, &date, &place))
	require.Equal(t, "245$a", title)
	require.Nil(t, publisher)
	require.Nil(t, date)
	require.Nil(t, place)
	down, err := os.ReadFile("../migrations/023_catalog_publication_metadata.down.sql")
	require.NoError(t, err)
	exec(string(down))
	require.NoError(t, tx.QueryRow(ctx, "SELECT metadata_marc21_title FROM catalog_configs").Scan(&title))
	require.Equal(t, "245$a", title)
}
