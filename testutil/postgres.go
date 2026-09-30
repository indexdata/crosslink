package testutil

import (
	"context"
	"fmt"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const postgresImage = "postgres:16"

// RunPostgres starts a PostgreSQL container with the settings shared by the
// Crosslink integration tests.
func RunPostgres(ctx context.Context) (*postgres.PostgresContainer, error) {
	if err := IsolateTestcontainersSession(); err != nil {
		return nil, fmt.Errorf("isolate Testcontainers session: %w", err)
	}
	return postgres.Run(ctx, postgresImage,
		postgres.WithDatabase("crosslink_test"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		postgres.BasicWaitStrategies(),
	)
}
