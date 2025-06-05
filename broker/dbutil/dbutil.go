package dbutil

import (
	"context"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
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
	if dirty && versionFrom == 8 {
		// we know that initial version of version 8 was bad, so we force it to version 7
		err = m.Force(7)
		if err != nil {
			return versionFrom, versionTo, dirty, fmt.Errorf("failed to force migration to version 7: %w", err)
		}
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
