package dbutil

import (
	"context"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func GetConnectionString(typ, user, pass, host, port, db string) string {
	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=disable", typ, user, pass, host, port, db)
}

func InitDbPool(connStr string) (*pgxpool.Pool, error) {
	return pgxpool.New(context.Background(), connStr)
}

func RunMigrateScripts(migrateDir, connStr string) (uint, uint, bool, error) {
	var versionFrom, versionTo uint
	var dirty bool
	m, err := migrate.New(migrateDir, connStr)
	if err != nil {
		return versionFrom, versionTo, dirty, fmt.Errorf("failed to initiate migration: %w", err)
	}
	// Check migration versionFrom before running
	versionFrom, dirty, err = m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return versionFrom, versionTo, dirty, fmt.Errorf("failed to get migration version: %w", err)
	}

	// Migrate up
	err = m.Up()
	if err != nil && err != migrate.ErrNoChange {
		return versionFrom, versionTo, dirty, fmt.Errorf("failed to run migration: %w", err)
	}

	// Check migration version after running
	versionTo, dirty, err = m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		return versionFrom, versionTo, dirty, fmt.Errorf("failed to get migration version after running: %w", err)
	}
	return versionFrom, versionTo, dirty, nil
}

func StartPGContainer() (context.Context, *postgres.PostgresContainer, string, error) {
	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	if err != nil {
		return ctx, pgContainer, "", fmt.Errorf("failed to start db container: %w", err)
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return ctx, pgContainer, "", fmt.Errorf("failed to get conn string: %w", err)
	}
	return ctx, pgContainer, connStr, nil
}

func TerminatePGContainer(ctx context.Context, pgContainer testcontainers.Container) error {
	if err := pgContainer.Terminate(ctx); err != nil {
		return fmt.Errorf("failed to stop db container: %w", err)
	}
	return nil
}
