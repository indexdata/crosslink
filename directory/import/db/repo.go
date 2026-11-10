package importdb

import (
	"context"
	"fmt"

	"github.com/indexdata/crosslink/directory/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/indexdata/crosslink/directory/policy"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgImportRepo struct {
	pool *pgxpool.Pool
	// inheritance rejects configuration that a record's entry type may not
	// store. Nil disables the check.
	inheritance *policy.Policy
}

func New(pool *pgxpool.Pool, inheritance *policy.Policy) *PgImportRepo {
	return &PgImportRepo{pool: pool, inheritance: inheritance}
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

func persistenceError(resource, key string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("persist %s %s aggregate: %w", resource, key, err)
}
