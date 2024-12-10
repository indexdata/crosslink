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

func (m *MockRepositorySuccess) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.IllTransaction, error) {
	return queries.IllTransaction{
		params.ID,
		params.Timestamp,
		params.RequesterSymbol,
		params.RequesterID,
		params.RequesterAction,
		params.SupplierSymbol,
		params.State,
		params.RequesterRequestID,
		params.SupplierRequestID,
		params.Data,
	}, nil
}

type MockRepositoryError struct {
	mock.Mock
}

func (m *MockRepositoryError) CreateIllTransaction(ctx context.Context, params queries.CreateIllTransactionParams) (queries.IllTransaction, error) {
	return queries.IllTransaction{}, errors.New("DB error")
}
