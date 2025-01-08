package repository

import (
	"context"
	"encoding/json"
	"fmt"

	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/db/model"
	"github.com/jackc/pgx/v5/pgtype"
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
