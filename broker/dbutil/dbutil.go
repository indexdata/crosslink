package dbutil

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"strings"
	"text/template"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/lib/pq"
)

var DB_SCHEMA = utils.GetEnv("DB_SCHEMA", "crosslink_broker")
var SchemaParam = "&search_path=" + DB_SCHEMA

func GetConnectionString(typ, user, pass, host, port, db string) string {
	return fmt.Sprintf("%s://%s:%s@%s:%s/%s?sslmode=disable"+SchemaParam, typ, user, pass, host, port, db)
}

func InitDbPool(connStr string) (*pgxpool.Pool, error) {
	return pgxpool.New(context.Background(), connStr)
}

func RunMigrateScripts(migrateDir, connStr string) (uint, uint, bool, error) {
	var versionFrom, versionTo uint
	var dirty bool
	err := initDBSchema(connStr)
	if err != nil {
		return versionFrom, versionTo, dirty, fmt.Errorf("failed to initiate schema: %w", err)
	}
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

func initDBSchema(connStr string) error {
	connStr = strings.Replace(connStr, SchemaParam, "", 1)
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("error opening database: : %w", err)
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
		Literal:    pq.QuoteLiteral(DB_SCHEMA),
		Identifier: pq.QuoteIdentifier(DB_SCHEMA),
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
