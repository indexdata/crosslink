package dbutil

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"text/template"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lib/pq"
)

func SearchPath(dbSchema string) string {
	if dbSchema == "" {
		return ""
	}
	return "&search_path=" + url.QueryEscape(dbSchema)
}

func GetConnectionString(typ, user, pass, host, port, db, dbSchema string) string {
	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=disable"+SearchPath(dbSchema), typ, user, pass, host, port, db)
}

func InitDbPool(connStr string) (*pgxpool.Pool, error) {
	return pgxpool.New(context.Background(), connStr)
}

func RunDbProvision(connStr, dbSchema string) error {
	if err := initDBSchema(connStr, dbSchema); err != nil {
		return fmt.Errorf("failed to initiate schema: %w", err)
	}
	return nil
}

func RunDbMigrations(migrateDir, connStr string) (uint, uint, bool, error) {
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

func initDBSchema(connStr, dbSchema string) error {
	if strings.TrimSpace(dbSchema) == "" {
		return fmt.Errorf("db schema must not be empty")
	}
	connStr, err := removeSearchPath(connStr)
	if err != nil {
		return fmt.Errorf("error removing search_path from connection string: %w", err)
	}
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("error opening database: %w", err)
	}
	defer db.Close()

	const setupSQL = `
        DO $$
        BEGIN
            IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = {{.Literal}}) THEN
                CREATE ROLE {{.Identifier}} WITH PASSWORD 'tenant' LOGIN;
                ALTER ROLE {{.Identifier}} SET search_path = {{.Identifier}};
                GRANT {{.Identifier}} TO CURRENT_USER;
            END IF;
        END
        $$;
        CREATE SCHEMA IF NOT EXISTS {{.Identifier}} AUTHORIZATION {{.Identifier}};`

	tmpl, err := template.New("setup").Parse(setupSQL)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	data := struct {
		Literal    string
		Identifier string
	}{
		Literal:    pq.QuoteLiteral(dbSchema),
		Identifier: pq.QuoteIdentifier(dbSchema),
	}

	if err = tmpl.Execute(&buf, data); err != nil {
		return err
	}

	_, err = db.Exec(buf.String())
	if err != nil {
		return fmt.Errorf("error executing script: %w", err)
	}

	return nil
}

func removeSearchPath(connStr string) (string, error) {
	parsed, err := url.Parse(connStr)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Del("search_path")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}
