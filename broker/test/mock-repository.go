package test

import (
	"context"
	"errors"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/stretchr/testify/mock"
)

type MockRepositorySuccess struct {
	mock.Mock
}

func (m *MockRepositorySuccess) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error) {
	var illTransaction = (queries.IllTransaction)(params)
	return queries.CreateIllTransactionRow{
		IllTransaction: illTransaction,
	}, nil
}

type MockRepositoryError struct {
	mock.Mock
}

func (m *MockRepositoryError) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.CreateIllTransactionRow, error) {
	return queries.CreateIllTransactionRow{}, errors.New("DB error")
}
