package mocks

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
)

type MockIllRepositorySuccess struct {
	mock.Mock
}

func (r *MockIllRepositorySuccess) GetIllTransactionById(ctx common.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID: id,
	}, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByIdForUpdate(ctx common.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID: id,
	}, nil
}

func (r *MockIllRepositorySuccess) SavePeer(ctx common.ExtendedContext, params ill_db.SavePeerParams) (ill_db.Peer, error) {
	var peer = (ill_db.Peer)(params)
	return peer, nil
}

func (r *MockIllRepositorySuccess) GetPeerById(ctx common.ExtendedContext, id string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID: id,
	}, nil
}

func (r *MockIllRepositorySuccess) GetRequesterByIllTransactionId(ctx common.ExtendedContext, illTransactionId string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID: uuid.NewString(),
	}, nil
}

func (r *MockIllRepositorySuccess) GetPeerBySymbol(ctx common.ExtendedContext, symbol string) (ill_db.Peer, error) {
	return ill_db.Peer{
		ID: uuid.New().String(),
	}, nil
}

func (r *MockIllRepositorySuccess) SaveLocatedSupplier(ctx common.ExtendedContext, params ill_db.SaveLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	var supplier = (ill_db.LocatedSupplier)(params)
	return supplier, nil
}

func (r *MockIllRepositorySuccess) GetLocatedSuppliersByIllTransactionAndStatus(ctx common.ExtendedContext, params ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{{
		ID:               uuid.New().String(),
		IllTransactionID: params.IllTransactionID,
		SupplierStatus:   params.SupplierStatus,
		SupplierID:       uuid.New().String(),
	}}, nil
}

func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx common.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams) (ill_db.LocatedSupplier, error) {
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

func (r *MockIllRepositorySuccess) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return fn(r)
}

func (m *MockIllRepositorySuccess) SaveIllTransaction(ctx common.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	var illTransaction = (ill_db.IllTransaction)(params)
	return illTransaction, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterRequestId(ctx common.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterRequestIdForUpdate(ctx common.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}

func (r *MockIllRepositorySuccess) ListIllTransactions(ctx common.ExtendedContext, params ill_db.ListIllTransactionsParams, cql *string) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{{
		ID: "id",
	}}, 0, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionsByRequesterSymbol(ctx common.ExtendedContext, params ill_db.GetIllTransactionsByRequesterSymbolParams, cql *string) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{{
		ID: "id",
	}}, 0, nil
}

func (r *MockIllRepositorySuccess) ListPeers(ctx common.ExtendedContext, params ill_db.ListPeersParams, cql *string) ([]ill_db.Peer, int64, error) {
	return []ill_db.Peer{{
		ID: uuid.New().String(),
	}}, 0, nil
}
func (r *MockIllRepositorySuccess) DeletePeer(ctx common.ExtendedContext, id string) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetSelectedSupplierForIllTransaction(ctx common.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{
		SupplierSymbol: "ISIL:SUP",
	}, nil
}

func (r *MockIllRepositorySuccess) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	return []ill_db.Peer{{ID: uuid.NewString()}}, "", nil
}

func (r *MockIllRepositorySuccess) GetLocatedSuppliersByIllTransaction(ctx common.ExtendedContext, id string) ([]ill_db.LocatedSupplier, int64, error) {
	return []ill_db.LocatedSupplier{{ID: uuid.NewString(), IllTransactionID: id}}, 0, nil
}

func (r *MockIllRepositorySuccess) SaveSymbol(ctx common.ExtendedContext, params ill_db.SaveSymbolParams) (ill_db.Symbol, error) {
	return ill_db.Symbol(params), nil
}

func (r *MockIllRepositorySuccess) DeleteSymbolByPeerId(ctx common.ExtendedContext, peerId string) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.Symbol, error) {
	return []ill_db.Symbol{{
		SymbolValue: "ISIL:SUP1",
		PeerID:      peerId,
	}}, nil
}

func (r *MockIllRepositorySuccess) DeleteLocatedSupplierByIllTransaction(ctx common.ExtendedContext, illTransId string) error {
	return nil
}

func (r *MockIllRepositorySuccess) DeleteIllTransaction(ctx common.ExtendedContext, id string) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterId(ctx common.ExtendedContext, peerId pgtype.Text) ([]ill_db.IllTransaction, error) {
	return []ill_db.IllTransaction{}, nil
}
func (r *MockIllRepositorySuccess) GetLocatedSupplierByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, nil
}

func (r *MockIllRepositorySuccess) SaveBranchSymbol(ctx common.ExtendedContext, params ill_db.SaveBranchSymbolParams) (ill_db.BranchSymbol, error) {
	return ill_db.BranchSymbol(params), nil
}
func (r *MockIllRepositorySuccess) GetBranchSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.BranchSymbol, error) {
	return []ill_db.BranchSymbol{{PeerID: peerId, SymbolValue: "ISIL:S1"}}, nil
}
func (r *MockIllRepositorySuccess) DeleteBranchSymbolByPeerId(ctx common.ExtendedContext, peerId string) error {
	return nil
}
func (r *MockIllRepositorySuccess) CallArchiveIllTransactionByDateAndStatus(ctx common.ExtendedContext, toDate time.Time, statuses []string) error {
	return errors.New("DB error")
}

// Implement missing method for interface compliance
func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransactionAndSymbol(ctx common.ExtendedContext, illTransactionId string, symbol string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{
		ID:               uuid.NewString(),
		IllTransactionID: illTransactionId,
		SupplierSymbol:   symbol,
	}, nil
}
func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransactionAndSymbolForUpdate(ctx common.ExtendedContext, illTransactionId string, symbol string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{
		ID:               uuid.NewString(),
		IllTransactionID: illTransactionId,
		SupplierSymbol:   symbol,
	}, nil
}

func (r *MockIllRepositorySuccess) GetExclusiveBranchSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.BranchSymbol, error) {
	return []ill_db.BranchSymbol{{SymbolValue: "ISIL:SUP1", PeerID: peerId}}, nil
}

func (r *MockIllRepositorySuccess) GetLocatedSupplierByIdForUpdate(ctx common.ExtendedContext, id string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{ID: id}, nil
}

type MockIllRepositoryError struct {
	mock.Mock
}

func (r *MockIllRepositoryError) GetIllTransactionById(ctx common.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByIdForUpdate(ctx common.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) SavePeer(ctx common.ExtendedContext, params ill_db.SavePeerParams) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetPeerById(ctx common.ExtendedContext, id string) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetRequesterByIllTransactionId(ctx common.ExtendedContext, illTransactionId string) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetPeerBySymbol(ctx common.ExtendedContext, symbol string) (ill_db.Peer, error) {
	return ill_db.Peer{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) SaveLocatedSupplier(ctx common.ExtendedContext, params ill_db.SaveLocatedSupplierParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSuppliersByIllTransactionAndStatus(ctx common.ExtendedContext, params ill_db.GetLocatedSuppliersByIllTransactionAndStatusParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx common.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	err := fn(r)
	if err != nil {
		return err
	}
	return errors.New("DB error")
}

func (m *MockIllRepositoryError) SaveIllTransaction(ctx common.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestId(ctx common.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestIdForUpdate(ctx common.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) ListIllTransactions(ctx common.ExtendedContext, params ill_db.ListIllTransactionsParams, cql *string) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionsByRequesterSymbol(ctx common.ExtendedContext, params ill_db.GetIllTransactionsByRequesterSymbolParams, cql *string) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) ListPeers(ctx common.ExtendedContext, params ill_db.ListPeersParams, cql *string) ([]ill_db.Peer, int64, error) {
	return []ill_db.Peer{{}}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) DeletePeer(ctx common.ExtendedContext, id string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetSelectedSupplierForIllTransaction(ctx common.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	return []ill_db.Peer{}, "", errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSuppliersByIllTransaction(ctx common.ExtendedContext, id string) ([]ill_db.LocatedSupplier, int64, error) {
	return []ill_db.LocatedSupplier{}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) SaveSymbol(ctx common.ExtendedContext, params ill_db.SaveSymbolParams) (ill_db.Symbol, error) {
	return ill_db.Symbol{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) DeleteSymbolByPeerId(ctx common.ExtendedContext, peerId string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.Symbol, error) {
	return []ill_db.Symbol{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) DeleteLocatedSupplierByIllTransaction(ctx common.ExtendedContext, illTransId string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) DeleteIllTransaction(ctx common.ExtendedContext, id string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterId(ctx common.ExtendedContext, peerId pgtype.Text) ([]ill_db.IllTransaction, error) {
	return []ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndSymbol(ctx common.ExtendedContext, illTransactionId string, symbol string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndSymbolForUpdate(ctx common.ExtendedContext, illTransactionId string, symbol string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) SaveBranchSymbol(ctx common.ExtendedContext, params ill_db.SaveBranchSymbolParams) (ill_db.BranchSymbol, error) {
	return ill_db.BranchSymbol{}, errors.New("DB error")
}
func (r *MockIllRepositoryError) GetBranchSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.BranchSymbol, error) {
	return []ill_db.BranchSymbol{}, errors.New("DB error")
}
func (r *MockIllRepositoryError) DeleteBranchSymbolByPeerId(ctx common.ExtendedContext, peerId string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) CallArchiveIllTransactionByDateAndStatus(ctx common.ExtendedContext, toDate time.Time, statuses []string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetExclusiveBranchSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.BranchSymbol, error) {
	return []ill_db.BranchSymbol{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIdForUpdate(ctx common.ExtendedContext, id string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}
