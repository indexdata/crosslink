package test

import (
	"errors"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

type MockIllRepositorySuccess struct {
	mock.Mock
	MockBaseRepo[ill_db.IllRepo]
}

func (m *MockIllRepositorySuccess) CreateIllTransaction(params ill_db.CreateIllTransactionParams) (ill_db.IllTransaction, error) {
	var illTransaction = (ill_db.IllTransaction)(params)
	return illTransaction, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}

type MockIllRepositoryError struct {
	mock.Mock
	MockBaseRepo[ill_db.IllRepo]
}

func (m *MockIllRepositoryError) CreateIllTransaction(params ill_db.CreateIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}
