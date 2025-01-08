package repository

import (
	"context"
	"encoding/json"
	"fmt"

	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/db/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository interface {
	BaseRepo
	CreateIllTransaction(params queries.CreateIllTransactionParams) (queries.IllTransaction, error)
	SaveEvent(params queries.SaveEventParams) (queries.Event, error)
	UpdateEventStatus(params queries.UpdateEventStatusParams) error
	GetEvent(id string) (queries.Event, error)
	GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (queries.IllTransaction, error)
	Notify(eventId string, signal model.Signal) error
}

type BaseRepo interface {
	//return underlying pool or active transaction
	GetPoolOrTx() queries.DBTX
	//return a new repo instance backed with the pool and, optionally, active transaction
	WithPoolAndTx(pool *pgxpool.Pool, tx pgx.Tx) Repository
	//execute handler within a transaction, handler receives a new repo instance with the original pool and new active transaction
	WithTxFunc(ctx context.Context, fn func(Repository) error) error
}

type PgBaseRepo struct {
	Pool *pgxpool.Pool
	Tx   pgx.Tx
}

type PostgresRepository struct {
	PgBaseRepo
	queries queries.Queries
}

func (r *PostgresRepository) CreateIllTransaction(params queries.CreateIllTransactionParams) (queries.IllTransaction, error) {
	row, err := r.queries.CreateIllTransaction(context.Background(), r.GetPoolOrTx(), params)
	return row.IllTransaction, err
}

func (r *PostgresRepository) SaveEvent(params queries.SaveEventParams) (queries.Event, error) {
	row, err := r.queries.SaveEvent(context.Background(), r.GetPoolOrTx(), params)
	return row.Event, err
}

func (r *PostgresRepository) GetEvent(id string) (queries.Event, error) {
	row, err := r.queries.GetEvent(context.Background(), r.GetPoolOrTx(), id)
	return row.Event, err
}

func (r *PostgresRepository) UpdateEventStatus(params queries.UpdateEventStatusParams) error {
	return r.queries.UpdateEventStatus(context.Background(), r.GetPoolOrTx(), params)
}

func (r *PostgresRepository) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (queries.IllTransaction, error) {
	row, err := r.queries.GetIllTransactionByRequesterRequestId(context.Background(), r.GetPoolOrTx(), requesterRequestID)
	return row.IllTransaction, err
}

func (r *PostgresRepository) Notify(eventId string, signal model.Signal) error {
	data := model.NotifyData{
		Event:  eventId,
		Signal: signal,
	}
	jsonData, _ := json.Marshal(data)
	sql := fmt.Sprintf("NOTIFY crosslink_channel, '%s'", jsonData)
	_, err := r.GetPoolOrTx().Exec(context.Background(), sql)
	return err
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

func (r *PgBaseRepo) GetPoolOrTx() queries.DBTX {
	if r.Tx != nil {
		return r.Tx //return active tx if any
	} else {
		return r.Pool //otherwise aquire from the pool
	}
}
