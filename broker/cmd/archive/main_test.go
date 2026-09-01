package main

import (
	"context"
	"os"
	"testing"

	"github.com/indexdata/crosslink/broker/app"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/testutil"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	app.DB_PROVISION = true

	pgContainer, err := testutil.RunPostgres(ctx)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"

	code := m.Run()

	test.Expect(test.TerminatePGContainer(ctx, pgContainer), "failed to stop db container")
	os.Exit(code)
}

func TestMainOK(t *testing.T) {
	err := run()
	assert.NoError(t, err)
}
