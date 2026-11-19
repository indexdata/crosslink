// Package ill_db contains generated ILL database models and queries.
//
//go:generate make -C .. generate-sqlc
package ill_db

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/indexdata/crosslink/directory/api"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/pgtype"
)
