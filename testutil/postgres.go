package testutil

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const postgresImage = "postgres:16"

// RunPostgres starts a PostgreSQL container with the settings shared by the
// Crosslink integration tests.
func RunPostgres(ctx context.Context) (*postgres.PostgresContainer, error) {
	startupCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := PrepareTestcontainers(startupCtx); err != nil {
		return nil, fmt.Errorf("prepare Testcontainers: %w", err)
	}
	return postgres.Run(startupCtx, postgresImage,
		postgres.WithDatabase("crosslink_test"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		postgres.BasicWaitStrategies(),
	)
}

// PostgresConnectionString resolves a test database endpoint with bounded
// connection establishment and SQL execution. Explicit args override the defaults.
func PostgresConnectionString(ctx context.Context, container *postgres.PostgresContainer, args ...string) (string, error) {
	endpointCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	connStr, err := container.ConnectionString(endpointCtx, args...)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(connStr)
	if err != nil {
		return "", fmt.Errorf("parse test database connection string: %w", err)
	}
	query := parsed.Query()
	if !query.Has("connect_timeout") {
		query.Set("connect_timeout", "10")
	}
	if !query.Has("statement_timeout") {
		query.Set("statement_timeout", "30000")
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
