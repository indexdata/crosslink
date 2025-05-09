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

func (r *MockIllRepositorySuccess) GetIllTransactionByIdForUpdate(ctx extctx.ExtendedContext, id string) (ill_db.IllTransaction, error) {
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
		ID: uuid.New().String(),
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

func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransactionAndStatusForUpdate(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndStatusForUpdateParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{{
		ID:               uuid.New().String(),
		IllTransactionID: params.IllTransactionID,
		SupplierStatus:   params.SupplierStatus,
		SupplierID:       uuid.New().String(),
	}}, nil
}

func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams) (ill_db.LocatedSupplier, error) {
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
	return fn(r)
}

func (m *MockIllRepositorySuccess) SaveIllTransaction(ctx extctx.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	var illTransaction = (ill_db.IllTransaction)(params)
	return illTransaction, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterRequestIdForUpdate(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{
		ID:                 "id",
		RequesterRequestID: requesterRequestID,
	}, nil
}

func (r *MockIllRepositorySuccess) ListIllTransactions(ctx extctx.ExtendedContext, params ill_db.ListIllTransactionsParams) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{{
		ID: "id",
	}}, 0, nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionsByRequesterSymbol(ctx extctx.ExtendedContext, params ill_db.GetIllTransactionsByRequesterSymbolParams) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{{
		ID: "id",
	}}, 0, nil
}

func (r *MockIllRepositorySuccess) ListPeers(ctx extctx.ExtendedContext, params ill_db.ListPeersParams) ([]ill_db.Peer, int64, error) {
	return []ill_db.Peer{{
		ID: uuid.New().String(),
	}}, 0, nil
}

func (r *MockIllRepositorySuccess) GetPeersWithSymbols(ctx extctx.ExtendedContext, params ill_db.GetPeersWithSymbolsParams) ([]ill_db.PeerWithSymbols, int64, error) {
	pi := ill_db.PeerWithSymbols{
		Peer: ill_db.Peer{
			ID: uuid.New().String(),
		},
		Symbols: []string{
			"ISIL:SUP1",
		},
	}
	return []ill_db.PeerWithSymbols{pi}, 0, nil
}

func (r *MockIllRepositorySuccess) DeletePeer(ctx extctx.ExtendedContext, id string) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, nil
}

func (r *MockIllRepositorySuccess) GetSelectedSupplierForIllTransactionForUpdate(ctx extctx.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, nil
}

func (r *MockIllRepositorySuccess) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string) {
	return []ill_db.Peer{{ID: uuid.NewString()}}, ""
}

func (r *MockIllRepositorySuccess) GetLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, id string) ([]ill_db.LocatedSupplier, int64, error) {
	return []ill_db.LocatedSupplier{{ID: uuid.NewString(), IllTransactionID: id}}, 0, nil
}

func (r *MockIllRepositorySuccess) SaveSymbol(ctx extctx.ExtendedContext, params ill_db.SaveSymbolParams) (ill_db.Symbol, error) {
	return ill_db.Symbol(params), nil
}

func (r *MockIllRepositorySuccess) DeleteSymbolByPeerId(ctx extctx.ExtendedContext, peerId string) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetSymbolsByPeerId(ctx extctx.ExtendedContext, peerId string) ([]ill_db.Symbol, error) {
	return []ill_db.Symbol{{
		SymbolValue: "ISIL:SUP1",
		PeerID:      peerId,
	}}, nil
}

func (r *MockIllRepositorySuccess) DeleteLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error {
	return nil
}

func (r *MockIllRepositorySuccess) DeleteIllTransaction(ctx extctx.ExtendedContext, id string) error {
	return nil
}

func (r *MockIllRepositorySuccess) GetIllTransactionByRequesterId(ctx extctx.ExtendedContext, peerId pgtype.Text) ([]ill_db.IllTransaction, error) {
	return []ill_db.IllTransaction{}, nil
}
func (r *MockIllRepositorySuccess) GetLocatedSupplierByPeerId(ctx extctx.ExtendedContext, peerId string) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, nil
}

type MockIllRepositoryError struct {
	mock.Mock
}

func (r *MockIllRepositoryError) GetIllTransactionById(ctx extctx.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByIdForUpdate(ctx extctx.ExtendedContext, id string) (ill_db.IllTransaction, error) {
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

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndStatusForUpdate(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndStatusForUpdateParams) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx extctx.ExtendedContext, params ill_db.GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) WithTxFunc(ctx extctx.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	err := fn(r)
	if err != nil {
		return err
	}
	return errors.New("DB error")
}

func (m *MockIllRepositoryError) SaveIllTransaction(ctx extctx.ExtendedContext, params ill_db.SaveIllTransactionParams) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterRequestIdForUpdate(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (ill_db.IllTransaction, error) {
	return ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) ListIllTransactions(ctx extctx.ExtendedContext, params ill_db.ListIllTransactionsParams) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionsByRequesterSymbol(ctx extctx.ExtendedContext, params ill_db.GetIllTransactionsByRequesterSymbolParams) ([]ill_db.IllTransaction, int64, error) {
	return []ill_db.IllTransaction{}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) ListPeers(ctx extctx.ExtendedContext, params ill_db.ListPeersParams) ([]ill_db.Peer, int64, error) {
	return []ill_db.Peer{{}}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetPeersWithSymbols(ctx extctx.ExtendedContext, params ill_db.GetPeersWithSymbolsParams) ([]ill_db.PeerWithSymbols, int64, error) {
	return []ill_db.PeerWithSymbols{{}}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) DeletePeer(ctx extctx.ExtendedContext, id string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetSelectedSupplierForIllTransactionForUpdate(ctx extctx.ExtendedContext, illTransId string) (ill_db.LocatedSupplier, error) {
	return ill_db.LocatedSupplier{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string) {
	return []ill_db.Peer{}, ""
}

func (r *MockIllRepositoryError) GetLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, id string) ([]ill_db.LocatedSupplier, int64, error) {
	return []ill_db.LocatedSupplier{}, 0, errors.New("DB error")
}

func (r *MockIllRepositoryError) SaveSymbol(ctx extctx.ExtendedContext, params ill_db.SaveSymbolParams) (ill_db.Symbol, error) {
	return ill_db.Symbol{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) DeleteSymbolByPeerId(ctx extctx.ExtendedContext, peerId string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetSymbolsByPeerId(ctx extctx.ExtendedContext, peerId string) ([]ill_db.Symbol, error) {
	return []ill_db.Symbol{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) DeleteLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) DeleteIllTransaction(ctx extctx.ExtendedContext, id string) error {
	return errors.New("DB error")
}

func (r *MockIllRepositoryError) GetIllTransactionByRequesterId(ctx extctx.ExtendedContext, peerId pgtype.Text) ([]ill_db.IllTransaction, error) {
	return []ill_db.IllTransaction{}, errors.New("DB error")
}

func (r *MockIllRepositoryError) GetLocatedSupplierByPeerId(ctx extctx.ExtendedContext, peerId string) ([]ill_db.LocatedSupplier, error) {
	return []ill_db.LocatedSupplier{}, errors.New("DB error")
}
