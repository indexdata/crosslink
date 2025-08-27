package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/app"
	test "github.com/indexdata/crosslink/broker/test/utils"
	_ "github.com/lib/pq" // PostgreSQL driver
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestCallArchive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := app.Archive(ctx, "Unfilled", "24h")
	assert.NoError(t, err)
}

func TestDelayUnitD(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := app.Archive(ctx, "Unfilled", "1d")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown unit \"d\"")
}

func TestDelayUnitW(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := app.Archive(ctx, "Unfilled", "1w")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown unit \"w\"")
}

func TestInitFail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.MigrationsFolder = "file://not-such-file"
	err := app.Archive(ctx, "Unfilled", "24h")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open")
	app.MigrationsFolder = "file://../../migrations"
}

func TestMainOK(t *testing.T) {
	os.Args = []string{"cmd/archive", "-statuses", "Unfilled", "-duration", "24h"}
	err := run()
	assert.NoError(t, err)
}

func TestBadOption(t *testing.T) {
	os.Args = []string{"cmd/archive", "-expiredduration", "24h"}
	err := run()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown flag")
}
