package testutil

import (
	"context"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

type postgresEndpoint struct {
	testcontainers.Container
	deadline time.Time
}

func (c *postgresEndpoint) PortEndpoint(ctx context.Context, _, _ string) (string, error) {
	c.deadline, _ = ctx.Deadline()
	return "docker.example.org:5432", ctx.Err()
}

func TestPostgresConnectionStringTimeouts(t *testing.T) {
	endpoint := &postgresEndpoint{}
	container := &postgres.PostgresContainer{Container: endpoint}
	start := time.Now()
	connStr, err := PostgresConnectionString(context.Background(), container, "sslmode=disable")
	require.NoError(t, err)
	parsed, err := url.Parse(connStr)
	require.NoError(t, err)
	require.Equal(t, "docker.example.org:5432", parsed.Host)
	require.Equal(t, "10", parsed.Query().Get("connect_timeout"))
	require.Equal(t, "30000", parsed.Query().Get("statement_timeout"))
	require.Equal(t, "disable", parsed.Query().Get("sslmode"))
	require.WithinDuration(t, start.Add(10*time.Second), endpoint.deadline, time.Second)
	connStr, err = PostgresConnectionString(context.Background(), container, "connect_timeout=20", "statement_timeout=0")
	require.NoError(t, err)
	parsed, err = url.Parse(connStr)
	require.NoError(t, err)
	require.Equal(t, "20", parsed.Query().Get("connect_timeout"))
	require.Equal(t, "0", parsed.Query().Get("statement_timeout"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = PostgresConnectionString(ctx, container)
	require.ErrorIs(t, err, context.Canceled)
}
