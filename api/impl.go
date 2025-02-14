package api

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"indexdata/directoryish/db"
)

type ApiImpl struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// Make sure we conform to StrictServerInterface
var _ StrictServerInterface = (*ApiImpl)(nil)

func NewApiImpl(pool *pgxpool.Pool, queries *db.Queries) ApiImpl {
	return ApiImpl{pool: pool, queries: queries}
}
