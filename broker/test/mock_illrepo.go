package test

import (
	"errors"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"

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

func (r *MockIllRepositorySuccess) SavePeer(ctx extctx.ExtendedContext, params ill_db.SavePeerParams) (ill_db.Peer, error) {
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

func (r *MockIllRepositorySuccess) SaveLocatedSupplier(ctx extctx.ExtendedContext, params ill_db.SaveLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
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

func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransactionAndSupplier(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndSupplierParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{
		ID:               uuid.New().String(),
		IllTransactionID: params.IllTransactionID,
		SupplierStatus: pgtype.Text{
			String: "new",
			Valid:  true,
		},
		SupplierID: uuid.New().String(),
	}, nil
}

func (r *MockIllRepositorySuccess) WithTxFunc(ctx extctx.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return nil
}

func (m *MockIllRepositorySuccess) SaveIllTransaction(ctx extctx.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	var illTransaction = (ill_db.IllTransaction)(params)
	return illTransaction, nil
}

func (m *MockIllRepositorySuccess) UpdateSupplierStatus(ctx extctx.ExtendedContext, params ill_db.UpdateSupplierStatusParams) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}
func (r *MockIllRepositorySuccess) ListIllTransactions(ctx extctx.ExtendedContext) ([]ill_db.IllTransaction, error) {
	return []ill_db.IllTransaction{{
		ID: "id",
	}}, nil
}
func (r *MockIllRepositorySuccess) ListPeers(ctx extctx.ExtendedContext) ([]ill_db.Peer, error) {
	return []ill_db.Peer{{
		ID: uuid.New().String(),
	}}, nil
}
func (r *MockIllRepositorySuccess) DeletePeer(ctx extctx.ExtendedContext, id string) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, nil
}

func (r *MockIllRepositorySuccess) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) []ill_db.Peer {
	return []ill_db.Peer{{ID: uuid.NewString()}}
}

type MockIllRepositoryError struct {
	mock.Mock
}

func (r *MockIllRepositoryError) GetIllTransactionById(ctx extctx.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) SavePeer(ctx extctx.ExtendedContext, params ill_db.SavePeerParams) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetPeerById(ctx extctx.ExtendedContext, id string) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) SaveLocatedSupplier(ctx extctx.ExtendedContext, params ill_db.SaveLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndStatus(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndSupplier(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndSupplierParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) WithTxFunc(ctx extctx.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return errors.New("DB error")
}

func (m *MockIllRepositoryError) SaveIllTransaction(ctx extctx.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (m *MockIllRepositoryError) UpdateSupplierStatus(ctx extctx.ExtendedContext, params ill_db.UpdateSupplierStatusParams) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}
func (r *MockIllRepositoryError) ListIllTransactions(ctx extctx.ExtendedContext) ([]ill_db.IllTransaction, error) {
	return []ill_db.IllTransaction{}, errors.New("DB error")
}
func (r *MockIllRepositoryError) ListPeers(ctx extctx.ExtendedContext) ([]ill_db.Peer, error) {
	return []ill_db.Peer{{}}, errors.New("DB error")
}
func (r *MockIllRepositoryError) DeletePeer(ctx extctx.ExtendedContext, id string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) []ill_db.Peer {
	return []ill_db.Peer{}
}
