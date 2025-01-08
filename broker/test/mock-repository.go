package test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	repository "github.com/indexdata/crosslink/broker/db"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/db/model"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/mock"
	"time"
)

type MockRepositorySuccess struct {
	mock.Mock
	repository.Repository
}

func (m *MockRepositorySuccess) CreateIllTransaction(params queries.CreateIllTransactionParams) (queries.IllTransaction, error) {
	var illTransaction = (queries.IllTransaction)(params)
	return illTransaction, nil
}

func (r *MockRepositorySuccess) SaveEvent(params queries.SaveEventParams) (queries.Event, error) {
	var event = (queries.Event)(params)
	return event, nil
}

func (r *MockRepositorySuccess) UpdateEventStatus(params queries.UpdateEventStatusParams) error {
	return nil
}

func (r *MockRepositorySuccess) GetEvent(id string) (queries.Event, error) {
	if id == "t-1-n" {
		return queries.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        getNow(),
			EventType:        model.EventTypeTask,
			EventName:        model.EventNameRequestReceived,
			EventStatus:      model.EventStatusNew,
		}, nil
	} else if id == "t-1-p" {
		return queries.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        getNow(),
			EventType:        model.EventTypeTask,
			EventName:        model.EventNameRequestReceived,
			EventStatus:      model.EventStatusProcessing,
		}, nil
	} else {
		return queries.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        getNow(),
			EventType:        model.EventTypeNotice,
			EventName:        model.EventNameRequesterMsgReceived,
			EventStatus:      model.EventStatusSuccess,
		}, nil
	}
}

func (r *MockRepositorySuccess) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (queries.IllTransaction, error) {
	return queries.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}

func (r *MockRepositorySuccess) Notify(eventId string, signal model.Signal) error {
	return nil
}

func (r *MockRepositorySuccess) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return nil
}

func (r *MockRepositorySuccess) Clone(txConn *pgxpool.Conn, txQueries *queries.Queries) repository.Repository {
	return r
}

func (r *MockRepositorySuccess) GetDbConnection() *pgxpool.Conn {
	return nil
}
func (r *MockRepositorySuccess) GetDbQueries() *queries.Queries {
	return nil
}

type MockRepositoryError struct {
	mock.Mock
	repository.Repository
}

func (m *MockRepositoryError) CreateIllTransaction(params queries.CreateIllTransactionParams) (queries.IllTransaction, error) {
	return queries.IllTransaction{}, errors.New("DB error")
}

func (r *MockRepositoryError) SaveEvent(params queries.SaveEventParams) (queries.Event, error) {
	return queries.Event{}, errors.New("DB error")
}

func (r *MockRepositoryError) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (queries.IllTransaction, error) {
	return queries.IllTransaction{}, errors.New("DB error")
}

func (r *MockRepositoryError) GetEvent(id string) (queries.Event, error) {
	return queries.Event{}, errors.New("DB error")
}

func (r *MockRepositoryError) Notify(eventId string, signal model.Signal) error {
	return errors.New("DB error")
}

func (r *MockRepositoryError) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return errors.New("DB error")
}

func (r *MockRepositoryError) Clone(txConn *pgxpool.Conn, txQueries *queries.Queries) repository.Repository {
	return r
}

func (r *MockRepositoryError) GetDbConnection() *pgxpool.Conn {
	return nil
}
func (r *MockRepositoryError) GetDbQueries() *queries.Queries {
	return nil
}

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
