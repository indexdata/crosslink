package repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DBTX interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type BaseRepo[T any] interface {
	//return underlying pool or active transaction
	GetPoolOrTx() DBTX
	//return a new instance backed with the new pool and, optionally, active transaction
	WithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) BaseRepo[T]
	//execute handler within a transaction, handler receives a new repo instance with the original pool and new active transaction
	WithTxFunc(ctx context.Context, repo DerivedRepo[T], fn func(T) error) error
}

type DerivedRepo[T any] interface {
	//create a new instance of the repo T backed by provided BaseRepo
	CreateWithBaseRepo(repo BaseRepo[T]) T
}

type PgBaseRepo[T any] struct {
	Pool *pgxpool.Pool
	Tx   pgx.Tx
}

func (r *PgBaseRepo[T]) WithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) BaseRepo[T] {
	return &PgBaseRepo[T]{
		Pool: pool,
		Tx:   tx,
	}
}

func (r *PgBaseRepo[T]) WithTxFunc(ctx context.Context, repo DerivedRepo[T], fn func(T) error) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("db tx rollback")
			_ = tx.Rollback(ctx)
			panic(r)
		} else if err != nil {
			fmt.Println("db tx error and rollback:", err)
			_ = tx.Rollback(ctx)
		} else {
			fmt.Println("db tx commit")
			err = tx.Commit(ctx)
		}
	}()
	newBase := r.WithPoolAndTx(r.Pool, tx)
	newRepo := repo.CreateWithBaseRepo(newBase)
	err = fn(newRepo)
	return err
}

func (r *PgBaseRepo[T]) GetPoolOrTx() DBTX {
	if r.Tx != nil {
		return r.Tx //return active tx if any
	} else {
		return r.Pool //otherwise aquire from the pool
	}
}
