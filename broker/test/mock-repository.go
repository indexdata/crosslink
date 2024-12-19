package test

import (
	"context"
	"errors"
	repository "github.com/indexdata/crosslink/broker/db"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"time"
)

type MockRepositorySuccess struct {
	mock.Mock
	repository.Repository
}

func (m *MockRepositorySuccess) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error) {
	var illTransaction = (queries.IllTransaction)(params)
	return queries.CreateIllTransactionRow{
		IllTransaction: illTransaction,
	}, nil
}

func (r *MockRepositorySuccess) CreateEvent(ctx context.Context, params queries.CreateEventParams) (queries.CreateEventRow, error) {
	var event = queries.Event{
		ID:               params.ID,
		IllTransactionID: params.IllTransactionID,
		EventType:        params.EventType,
		EventName:        params.EventName,
		EventStatus:      params.EventStatus,
		EventData:        params.EventData,
		ResultData:       params.ResultData,
		Timestamp: pgtype.Timestamp{
			Time: time.Now(),
		},
	}
	return queries.CreateEventRow{
		Event: event,
	}, nil
}

func (r *MockRepositorySuccess) GetIllTransactionByRequesterRequestId(ctx context.Context, requesterRequestID pgtype.Text) (queries.GetIllTransactionByRequesterRequestIdRow, error) {
	var trans = queries.GetIllTransactionByRequesterRequestIdRow{
		IllTransaction: queries.IllTransaction{
			ID:                 "id",
			RequesterRequestID: requesterRequestID,
		},
	}
	return trans, nil
}

type MockRepositoryError struct {
	mock.Mock
	repository.Repository
}

func (m *MockRepositoryError) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error) {
	return queries.CreateIllTransactionRow{}, errors.New("DB error")
}

func (r *MockRepositoryError) CreateEvent(ctx context.Context, params queries.CreateEventParams) (queries.CreateEventRow, error) {
	return queries.CreateEventRow{}, errors.New("DB error")
}

func (r *MockRepositoryError) GetIllTransactionByRequesterRequestId(ctx context.Context, requesterRequestID pgtype.Text) (queries.GetIllTransactionByRequesterRequestIdRow, error) {
	return queries.GetIllTransactionByRequesterRequestIdRow{}, errors.New("DB error")
}
