package test

import (
	"errors"
	"github.com/google/uuid"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

type MockIllRepositorySuccess struct {
	mock.Mock
}

func (r *MockIllRepositorySuccess) GetIllTransactionById(ctx extctx.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID: id,
	}, nil
}

func (r *MockIllRepositorySuccess) CreatePeer(ctx extctx.ExtendedContext, params ill_db.CreatePeerParams) (ill_db.Peer, error) {
	var peer = (ill_db.Peer)(params)
	return peer, nil
}

func (r *MockIllRepositorySuccess) GetPeerById(ctx extctx.ExtendedContext, id string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID: id,
	}, nil
}

func (r *MockIllRepositorySuccess) GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID:     uuid.New().String(),
		Symbol: symbol,
	}, nil
}

func (r *MockIllRepositorySuccess) CreateLocatedSupplier(ctx extctx.ExtendedContext, params ill_db.CreateLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	var supplier = (ill_db.LocatedSupplier)(params)
	return supplier, nil
}

func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransactionAndStatus(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{{
		ID:               uuid.New().String(),
		IllTransactionID: params.IllTransactionID,
		SupplierStatus:   params.SupplierStatus,
		SupplierID:       uuid.New().String(),
	}}, nil
}

func (r *MockIllRepositorySuccess) WithTxFunc(ctx extctx.ExtendedContext, fn func(ill_db.IllRepo) error) error {
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

func (r *MockIllRepositoryError) GetIllTransactionById(ctx extctx.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) CreatePeer(ctx extctx.ExtendedContext, params ill_db.CreatePeerParams) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetPeerById(ctx extctx.ExtendedContext, id string) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) CreateLocatedSupplier(ctx extctx.ExtendedContext, params ill_db.CreateLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndStatus(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) WithTxFunc(ctx extctx.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return nil
}

func (m *MockIllRepositoryError) CreateIllTransaction(ctx extctx.ExtendedContext, params ill_db.CreateIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}
