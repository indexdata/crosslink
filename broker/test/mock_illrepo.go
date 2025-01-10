package test

import (
	"context"
	"errors"

	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

type MockIllRepositorySuccess struct {
	mock.Mock
	//MockBaseRepo[ill_db.IllRepo]
}

func (r *MockIllRepositorySuccess) CreateWithBaseRepo(repo repo.BaseRepo[ill_db.IllRepo]) ill_db.IllRepo {
	return nil
}

func (r *MockIllRepositorySuccess) WithTxFunc(ctx context.Context, fn func(ill_db.IllRepo) error) error {
	return nil
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
	//MockBaseRepo[ill_db.IllRepo]
}

func (r *MockIllRepositoryError) CreateWithBaseRepo(repo repo.BaseRepo[ill_db.IllRepo]) ill_db.IllRepo {
	return nil
}

func (r *MockIllRepositoryError) WithTxFunc(ctx context.Context, fn func(ill_db.IllRepo) error) error {
	return nil
}

func (m *MockIllRepositoryError) CreateIllTransaction(params ill_db.CreateIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}
