// Package pr_db contains generated patron-request database models and queries.
//
//go:generate make -C ../.. generate-sqlc
package pr_db

import (
	// Retain the generated code's external runtime dependencies before generation.
	_ "github.com/indexdata/crosslink/iso18626"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/pgtype"
)
