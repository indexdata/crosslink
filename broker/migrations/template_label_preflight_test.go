package migrations_test

import (
	"context"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/indexdata/crosslink/broker/dbutil"
	"github.com/indexdata/crosslink/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestTemplateLabelUniquenessMigrationRejectsExistingOverlaps(t *testing.T) {
	ctx, migrator, pool := migrationAtVersion61(t)
	const owner = "ISIL:OVERLAPPING-OWNER"
	_, err := pool.Exec(ctx, `
		INSERT INTO template (id, owner, title, purpose, body, content_type, labels)
		VALUES
			('template-1', $1, 'First', 'email', 'body', 'text/plain', ARRAY['shared-a', 'shared-b']),
			('template-2', $1, 'Second', 'email', 'body', 'text/plain', ARRAY['shared-a']),
			('template-3', $1, 'Third', 'email', 'body', 'text/plain', ARRAY['shared-b'])`, owner)
	require.NoError(t, err)

	err = migrator.Migrate(62)

	require.Error(t, err)
	require.ErrorContains(t, err, owner)
	require.ErrorContains(t, err, "shared-a")
	require.ErrorContains(t, err, "shared-b")
}

func TestTemplateLabelUniquenessMigrationIgnoresNullArrayElements(t *testing.T) {
	ctx, migrator, pool := migrationAtVersion61(t)
	_, err := pool.Exec(ctx, `
		INSERT INTO template (id, owner, title, purpose, body, content_type, labels)
		VALUES
			('template-null-1', 'ISIL:NULL-OWNER', 'First', 'email', 'body', 'text/plain', ARRAY[NULL]::TEXT[]),
			('template-null-2', 'ISIL:NULL-OWNER', 'Second', 'email', 'body', 'text/plain', ARRAY[NULL]::TEXT[])`)
	require.NoError(t, err)

	require.NoError(t, migrator.Migrate(62))
}

func migrationAtVersion61(t *testing.T) (context.Context, *migrate.Migrate, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	container, err := testutil.RunPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, container.Terminate(context.Background()))
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	require.NoError(t, dbutil.RunDbProvision(connectionString, "crosslink_broker"))
	connectionString += dbutil.SearchPath("crosslink_broker")

	migrator, err := migrate.New("file://.", connectionString)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})
	require.NoError(t, migrator.Migrate(61))

	pool, err := pgxpool.New(ctx, connectionString)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return ctx, migrator, pool
}
