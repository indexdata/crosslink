// Package events contains generated broker event database models and queries.
//
//go:generate make -C .. generate-sqlc
package events

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/pgtype"
)
