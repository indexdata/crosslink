package api

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
)

type AggregateImporter interface {
	Import(context.Context, model.ConflictPolicy, io.Reader) (model.ImportResult, error)
}

type ApiImpl struct {
	pool     *pgxpool.Pool
	queries  *db.Queries
	importer AggregateImporter
}

// Make sure we conform to StrictServerInterface
var _ StrictServerInterface = (*ApiImpl)(nil)

func NewApiImpl(pool *pgxpool.Pool, queries *db.Queries, importer AggregateImporter) ApiImpl {
	return ApiImpl{pool: pool, queries: queries, importer: importer}
}
