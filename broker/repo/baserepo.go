package repo

import (
	"context"

	extctx "github.com/indexdata/crosslink/broker/common"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ConnOrTx interface {
	Exec(context.Context, string, ...interface{}) (pgconn.CommandTag, error)
	Query(context.Context, string, ...interface{}) (pgx.Rows, error)
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}

type Transactional[T any] interface {
	//execute operations on the receiver repo within a transaction
	WithTxFunc(ctx extctx.ExtendedContext, fn func(T) error) error
}

type PgDerivedRepo[T any] interface {
	//create a new instance of the repo T backed by the provided PgBaseRepo
	CreateWithPgBaseRepo(repo *PgBaseRepo[T]) T
}

type PgBaseRepo[T any] struct {
	Pool *pgxpool.Pool
	Tx   pgx.Tx
}

func (r *PgBaseRepo[T]) createWithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) *PgBaseRepo[T] {
	return &PgBaseRepo[T]{
		Pool: pool,
		Tx:   tx,
	}
}

func (r *PgBaseRepo[T]) WithTxFunc(ctx extctx.ExtendedContext, repo PgDerivedRepo[T], fn func(T) error) error {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			ctx.Logger().Error("DB transaction rollback due to panic", "error", r)
			_ = tx.Rollback(ctx)
			panic(r)
		} else if err != nil {
			ctx.Logger().Debug("DB transaction rollback due to error", "error", err)
			_ = tx.Rollback(ctx)
		} else {
			err = tx.Commit(ctx)
		}
	}()
	newBase := r.createWithPoolAndTx(r.Pool, tx)
	newRepo := repo.CreateWithPgBaseRepo(newBase)
	err = fn(newRepo)
	return err
}

func (r *PgBaseRepo[T]) GetConnOrTx() ConnOrTx {
	if r.Tx != nil {
		return r.Tx //return active tx if any
	} else {
		return r.Pool //otherwise aquire from the pool
	}
}
