// Package ps_db contains generated pull-slip database models and queries.
//
//go:generate make -C ../.. generate-sqlc
package ps_db

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/pgtype"
)
