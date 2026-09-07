package importdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgImportRepo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *PgImportRepo {
	return &PgImportRepo{pool: pool}
}

func (r *PgImportRepo) begin(ctx context.Context) (pgx.Tx, *db.Queries, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin directory import transaction: %w", err)
	}
	return tx, db.New(tx), nil
}

func conflictResult(resource, key string, policy model.ConflictPolicy) (model.RepoResult, error) {
	switch policy {
	case model.ConflictPolicyFail:
		return model.RepoResult{}, fmt.Errorf("%s %s already exists", resource, key)
	case model.ConflictPolicySkip:
		return model.RepoResult{Outcome: model.OutcomeSkipped, Diagnostic: fmt.Sprintf("%s %s already exists", resource, key)}, nil
	case model.ConflictPolicyUpdate:
		return model.RepoResult{}, nil
	default:
		return model.RepoResult{}, fmt.Errorf("invalid conflict policy")
	}
}

func resolveEntry(ctx context.Context, queries *db.Queries, key model.SymbolRef) (db.Entry, error) {
	entry, err := queries.EntryBySymbolForUpdate(ctx, db.EntryBySymbolForUpdateParams{Authority: key.Authority, Symbol: key.Symbol})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Entry{}, fmt.Errorf("entry %s does not exist", key.String())
	}
	if err != nil {
		return db.Entry{}, fmt.Errorf("resolve entry %s", key.String())
	}
	return entry, nil
}

func persistenceError(resource, key string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("persist %s %s aggregate", resource, key)
}
