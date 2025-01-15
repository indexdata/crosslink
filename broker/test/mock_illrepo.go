package test

import (
	"context"
	"errors"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

type MockIllRepositorySuccess struct {
	mock.Mock
}

func (r *MockIllRepositorySuccess) WithTxFunc(ctx context.Context, fn func(ill_db.IllRepo) error) error {
	return nil
}

func (m *MockIllRepositorySuccess) CreateIllTransaction(ctx extctx.ExtendedContext, params ill_db.CreateIllTransactionParams) (ill_db.IllTransaction, error) {
	var illTransaction = (ill_db.IllTransaction)(params)
	return illTransaction, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}

type MockIllRepositoryError struct {
	mock.Mock
}

func (r *MockIllRepositoryError) WithTxFunc(ctx context.Context, fn func(ill_db.IllRepo) error) error {
	return nil
}

func (m *MockIllRepositoryError) CreateIllTransaction(ctx extctx.ExtendedContext, params ill_db.CreateIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}
