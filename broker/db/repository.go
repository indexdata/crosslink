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
}

type PostgresRepository struct {
	DbPool *pgxpool.Pool
}

func (r *PostgresRepository) CreateIllTransaction(params queries.CreateIllTransactionParams) (queries.IllTransaction, error) {
	row, err := GetDbQueries(r.DbPool).CreateIllTransaction(context.Background(), params)
	return row.IllTransaction, err
}

func (r *PostgresRepository) SaveEvent(params queries.SaveEventParams) (queries.Event, error) {
	row, err := GetDbQueries(r.DbPool).SaveEvent(context.Background(), params)
	return row.Event, err
}

func (r *PostgresRepository) GetEvent(id string) (queries.Event, error) {
	row, err := GetDbQueries(r.DbPool).GetEvent(context.Background(), id)
	return row.Event, err
}

func (r *PostgresRepository) UpdateEventStatus(params queries.UpdateEventStatusParams) error {
	return GetDbQueries(r.DbPool).UpdateEventStatus(context.Background(), params)
}

func (r *PostgresRepository) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (queries.IllTransaction, error) {
	row, err := GetDbQueries(r.DbPool).GetIllTransactionByRequesterRequestId(context.Background(), requesterRequestID)
	return row.IllTransaction, err
}

func (r *PostgresRepository) Notify(eventId string, signal model.Signal) error {
	data := model.NotifyData{
		Event:  eventId,
		Signal: signal,
	}
	jsonData, _ := json.Marshal(data)
	sql := fmt.Sprintf("NOTIFY crosslink_channel, '%s'", jsonData)
	_, err := getDbConnection(r.DbPool).Exec(context.Background(), sql)
	return err
}

func GetDbQueries(dbPool *pgxpool.Pool) *queries.Queries {
	return queries.New(getDbConnection(dbPool))
}
func getDbConnection(dbPool *pgxpool.Pool) *pgxpool.Conn {
	con, err := dbPool.Acquire(context.Background())
	if err != nil {
		log.Fatalf("Unable to connect to database: %v\n", err)
	}
	return con
}
