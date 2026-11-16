package api

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/indexdata/crosslink/directory/policy"
)

type AggregateImporter interface {
	Import(context.Context, model.ConflictPolicy, io.Reader) (model.ImportResult, error)
}

type ApiImpl struct {
	pool     *pgxpool.Pool
	queries  *db.Queries
	importer AggregateImporter
	// inheritance enforces editability on writes and merges inherited
	// configuration on reads. Nil disables both.
	inheritance *policy.Policy
}

// Make sure we conform to StrictServerInterface
var _ StrictServerInterface = (*ApiImpl)(nil)

func NewApiImpl(pool *pgxpool.Pool, queries *db.Queries, importer AggregateImporter, inheritance *policy.Policy) ApiImpl {
	return ApiImpl{pool: pool, queries: queries, importer: importer, inheritance: inheritance}
}
