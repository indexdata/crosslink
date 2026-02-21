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
	app.DB_PROVISION = true

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

func TestMainOK(t *testing.T) {
	err := run()
	assert.NoError(t, err)
}
