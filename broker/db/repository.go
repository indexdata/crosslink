package repository

import (
	"context"
	"encoding/json"
	"fmt"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/db/model"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
)

type Repository interface {
	CreateIllTransaction(params queries.CreateIllTransactionParams) (queries.IllTransaction, error)
	SaveEvent(params queries.SaveEventParams) (queries.Event, error)
	UpdateEventStatus(params queries.UpdateEventStatusParams) error
	GetEvent(id string) (queries.Event, error)
	GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (queries.IllTransaction, error)
	Notify(eventId string, signal model.Signal) error
	Clone(txConn *pgxpool.Conn, txQueries *queries.Queries) Repository
	GetDbConnection() *pgxpool.Conn
	GetDbQueries() *queries.Queries
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type PostgresRepository struct {
	DbPool    *pgxpool.Pool
	TxQueries *queries.Queries
	TxConn    *pgxpool.Conn
}

func (r *PostgresRepository) CreateIllTransaction(params queries.CreateIllTransactionParams) (queries.IllTransaction, error) {
	row, err := r.GetDbQueries().CreateIllTransaction(context.Background(), params)
	return row.IllTransaction, err
}

func (r *PostgresRepository) SaveEvent(params queries.SaveEventParams) (queries.Event, error) {
	row, err := r.GetDbQueries().SaveEvent(context.Background(), params)
	return row.Event, err
}

func (r *PostgresRepository) GetEvent(id string) (queries.Event, error) {
	row, err := r.GetDbQueries().GetEvent(context.Background(), id)
	return row.Event, err
}

func (r *PostgresRepository) UpdateEventStatus(params queries.UpdateEventStatusParams) error {
	return r.GetDbQueries().UpdateEventStatus(context.Background(), params)
}

func (r *PostgresRepository) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (queries.IllTransaction, error) {
	row, err := r.GetDbQueries().GetIllTransactionByRequesterRequestId(context.Background(), requesterRequestID)
	return row.IllTransaction, err
}

func (r *PostgresRepository) Notify(eventId string, signal model.Signal) error {
	data := model.NotifyData{
		Event:  eventId,
		Signal: signal,
	}
	jsonData, _ := json.Marshal(data)
	sql := fmt.Sprintf("NOTIFY crosslink_channel, '%s'", jsonData)
	_, err := r.GetDbConnection().Exec(context.Background(), sql)
	return err
}

func (r *PostgresRepository) Clone(txConn *pgxpool.Conn, txQueries *queries.Queries) Repository {
	return &PostgresRepository{
		DbPool:    r.DbPool,
		TxConn:    txConn,
		TxQueries: txQueries,
	}
}

func (r *PostgresRepository) WithTx(ctx context.Context, fn func(Repository) error) error {
	txConn := r.GetDbConnection()
	tx, err := txConn.Begin(ctx)
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
	clone := r.Clone(txConn, queries.New(txConn).WithTx(tx))
	err = fn(clone)
	return err
}

func (r *PostgresRepository) GetDbQueries() *queries.Queries {
	if r.TxQueries != nil {
		return r.TxQueries
	}
	return queries.New(r.GetDbConnection())
}

func (r *PostgresRepository) GetDbConnection() *pgxpool.Conn {
	if r.TxConn != nil {
		return r.TxConn
	}
	con, err := r.DbPool.Acquire(context.Background())
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	return con
}
