package test

import (
	"context"

	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type MockBaseRepo[T any] struct {
}

func (r *MockBaseRepo[T]) CreateWithBaseRepo(repo repo.BaseRepo[T]) T {
	return *new(T)
}

func (r *MockBaseRepo[T]) GetPoolOrTx() repo.DBTX {
	return nil
}

func (r *MockBaseRepo[T]) WithTxFunc(ctx context.Context, repo repo.DerivedRepo[T], fn func(T) error) error {
	return nil
}

func (r *MockBaseRepo[T]) WithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) repo.BaseRepo[T] {
	return r
}
