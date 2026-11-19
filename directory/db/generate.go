// Package db contains generated Directory database models and queries.
//
//go:generate make -C .. generate-sqlc
package db

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/google/uuid"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/pgtype"
)
