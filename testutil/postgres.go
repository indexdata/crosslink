package testutil

import (
	"context"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const postgresImage = "postgres:16"

// RunPostgres starts a PostgreSQL container with the settings shared by the
// Crosslink integration tests.
func RunPostgres(ctx context.Context) (*postgres.PostgresContainer, error) {
	return postgres.Run(ctx, postgresImage,
		postgres.WithDatabase("crosslink_test"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
}
