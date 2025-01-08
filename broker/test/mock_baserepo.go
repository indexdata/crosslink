package test

import (
	"context"

	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MockBaseRepo struct {
}

func (r *MockBaseRepo) SetBaseRepo(repo repo.BaseRepo) {
}

func (r *MockBaseRepo) GetPoolOrTx() repo.DBTX {
	return nil
}

func (r *MockBaseRepo) WithTxFunc(ctx context.Context, repo repo.DerivedRepo, fn func(repo.DerivedRepo) error) error {
	return nil
}

func (r *MockBaseRepo) WithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) repo.BaseRepo {
	return r
}
