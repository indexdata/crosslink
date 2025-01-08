package repository

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

type BaseRepo interface {
	//return underlying pool or active transaction
	GetPoolOrTx() DBTX
	//return a new repo instance backed with the pool and, optionally, active transaction
	WithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) Repository
	//execute handler within a transaction, handler receives a new repo instance with the original pool and new active transaction
	WithTxFunc(ctx context.Context, fn func(Repository) error) error
}

type PgBaseRepo struct {
	Pool *pgxpool.Pool
	Tx   pgx.Tx
}

func (r *PgBaseRepo) WithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) Repository {
	return &PostgresRepository{
		PgBaseRepo: PgBaseRepo{
			Pool: pool,
			Tx:   tx,
		},
	}
}

func (r *PgBaseRepo) WithTxFunc(ctx context.Context, fn func(Repository) error) error {
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
	txRepo := r.WithPoolAndTx(r.Pool, tx)
	err = fn(txRepo)
	return err
}

func (r *PgBaseRepo) GetPoolOrTx() DBTX {
	if r.Tx != nil {
		return r.Tx //return active tx if any
	} else {
		return r.Pool //otherwise aquire from the pool
	}
}
