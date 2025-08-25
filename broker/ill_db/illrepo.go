package ill_db

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type IllRepo interface {
	repo.Transactional[IllRepo]
	SaveIllTransaction(ctx extctx.ExtendedContext, params SaveIllTransactionParams) (IllTransaction, error)
	GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error)
	GetIllTransactionByRequesterRequestIdForUpdate(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error)
	GetIllTransactionById(ctx extctx.ExtendedContext, id string) (IllTransaction, error)
	GetIllTransactionByIdForUpdate(ctx extctx.ExtendedContext, id string) (IllTransaction, error)
	ListIllTransactions(ctx extctx.ExtendedContext, params ListIllTransactionsParams, cql *string) ([]IllTransaction, int64, error)
	GetIllTransactionsByRequesterSymbol(ctx extctx.ExtendedContext, params GetIllTransactionsByRequesterSymbolParams, cql *string) ([]IllTransaction, int64, error)
	DeleteIllTransaction(ctx extctx.ExtendedContext, id string) error
	SavePeer(ctx extctx.ExtendedContext, params SavePeerParams) (Peer, error)
	GetPeerById(ctx extctx.ExtendedContext, id string) (Peer, error)
	GetRequesterByIllTransactionId(ctx extctx.ExtendedContext, illTransactionId string) (Peer, error)
	GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (Peer, error)
	ListPeers(ctx extctx.ExtendedContext, params ListPeersParams, cql *string) ([]Peer, int64, error)
	DeletePeer(ctx extctx.ExtendedContext, id string) error
	SaveLocatedSupplier(ctx extctx.ExtendedContext, params SaveLocatedSupplierParams) (LocatedSupplier, error)
	GetLocatedSuppliersByIllTransactionAndStatus(ctx extctx.ExtendedContext, params GetLocatedSuppliersByIllTransactionAndStatusParams) ([]LocatedSupplier, error)
	GetLocatedSuppliersByIllTransaction(ctx extctx.ExtendedContext, id string) ([]LocatedSupplier, int64, error)
	GetLocatedSuppliersByIllTransactionAndStatusForUpdate(ctx extctx.ExtendedContext, params GetLocatedSuppliersByIllTransactionAndStatusForUpdateParams) ([]LocatedSupplier, error)
	GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams) (LocatedSupplier, error)
	GetLocatedSupplierByIllTransactionAndSymbol(ctx extctx.ExtendedContext, id, symbol string) (LocatedSupplier, error)
	GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (LocatedSupplier, error)
	GetSelectedSupplierForIllTransactionForUpdate(ctx extctx.ExtendedContext, illTransId string) (LocatedSupplier, error)
	DeleteLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error
	GetLocatedSupplierByPeerId(ctx extctx.ExtendedContext, peerId string) ([]LocatedSupplier, error)
	GetIllTransactionByRequesterId(ctx extctx.ExtendedContext, peerId pgtype.Text) ([]IllTransaction, error)
	GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]Peer, string, error)
	SaveSymbol(ctx extctx.ExtendedContext, params SaveSymbolParams) (Symbol, error)
	DeleteSymbolByPeerId(ctx extctx.ExtendedContext, peerId string) error
	GetSymbolsByPeerId(ctx extctx.ExtendedContext, peerId string) ([]Symbol, error)
	SaveBranchSymbol(ctx extctx.ExtendedContext, params SaveBranchSymbolParams) (BranchSymbol, error)
	GetBranchSymbolsByPeerId(ctx extctx.ExtendedContext, peerId string) ([]BranchSymbol, error)
	DeleteBranchSymbolByPeerId(ctx extctx.ExtendedContext, peerId string) error
}

type PgIllRepo struct {
	repo.PgBaseRepo[IllRepo]
	queries Queries
}

// delegate transaction handling to Base
func (r *PgIllRepo) WithTxFunc(ctx extctx.ExtendedContext, fn func(IllRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// DerivedRepo
func (r *PgIllRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[IllRepo]) IllRepo {
	eventRepo := new(PgIllRepo)
	eventRepo.PgBaseRepo = *base
	return eventRepo
}

func (r *PgIllRepo) SaveIllTransaction(ctx extctx.ExtendedContext, params SaveIllTransactionParams) (IllTransaction, error) {
	row, err := r.queries.SaveIllTransaction(ctx, r.GetConnOrTx(), params)
	return row.IllTransaction, err
}

func (r *PgIllRepo) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error) {
	row, err := r.queries.GetIllTransactionByRequesterRequestId(ctx, r.GetConnOrTx(), requesterRequestID)
	return row.IllTransaction, err
}

func (r *PgIllRepo) GetIllTransactionByRequesterRequestIdForUpdate(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error) {
	row, err := r.queries.GetIllTransactionByRequesterRequestIdForUpdate(ctx, r.GetConnOrTx(), requesterRequestID)
	return row.IllTransaction, err
}

func (r *PgIllRepo) GetIllTransactionById(ctx extctx.ExtendedContext, id string) (IllTransaction, error) {
	row, err := r.queries.GetIllTransactionById(ctx, r.GetConnOrTx(), id)
	return row.IllTransaction, err
}

func (r *PgIllRepo) GetIllTransactionByIdForUpdate(ctx extctx.ExtendedContext, id string) (IllTransaction, error) {
	row, err := r.queries.GetIllTransactionByIdForUpdate(ctx, r.GetConnOrTx(), id)
	return row.IllTransaction, err
}

func (r *PgIllRepo) ListIllTransactions(ctx extctx.ExtendedContext, params ListIllTransactionsParams, cql *string) ([]IllTransaction, int64, error) {
	rows, err := r.queries.ListIllTransactionsCql(ctx, r.GetConnOrTx(), params, cql)
	var transactions []IllTransaction
	var fullCount int64
	if err == nil {
		if len(rows) > 0 {
			fullCount = rows[0].FullCount
			for _, r := range rows {
				fullCount = r.FullCount
				transactions = append(transactions, r.IllTransaction)
			}
		} else {
			params.Limit = 1
			params.Offset = 0
			rows, err = r.queries.ListIllTransactionsCql(ctx, r.GetConnOrTx(), params, cql)
			if err == nil && len(rows) > 0 {
				fullCount = rows[0].FullCount
			}
		}
	}
	return transactions, fullCount, err
}

func (r *PgIllRepo) GetIllTransactionsByRequesterSymbol(ctx extctx.ExtendedContext, params GetIllTransactionsByRequesterSymbolParams, cql *string) ([]IllTransaction, int64, error) {
	rows, err := r.queries.GetIllTransactionsByRequesterSymbolCql(ctx, r.GetConnOrTx(), params, cql)
	var transactions []IllTransaction
	var fullCount int64
	if err == nil {
		for _, r := range rows {
			fullCount = r.FullCount
			transactions = append(transactions, r.IllTransaction)
		}
	}
	return transactions, fullCount, err
}

func (r *PgIllRepo) DeleteIllTransaction(ctx extctx.ExtendedContext, id string) error {
	return r.queries.DeleteIllTransaction(ctx, r.GetConnOrTx(), id)
}

func (r *PgIllRepo) GetPeerById(ctx extctx.ExtendedContext, id string) (Peer, error) {
	row, err := r.queries.GetPeerById(ctx, r.GetConnOrTx(), id)
	return row.Peer, err
}

func (r *PgIllRepo) GetRequesterByIllTransactionId(ctx extctx.ExtendedContext, illTransactionId string) (Peer, error) {
	row, err := r.queries.GetRequesterByIllTransactionId(ctx, r.GetConnOrTx(), illTransactionId)
	return row.Peer, err
}

func (r *PgIllRepo) GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (Peer, error) {
	row, err := r.queries.GetPeerBySymbol(ctx, r.GetConnOrTx(), symbol)
	return row.Peer, err
}

func (r *PgIllRepo) ListPeers(ctx extctx.ExtendedContext, params ListPeersParams, cql *string) ([]Peer, int64, error) {
	rows, err := r.queries.ListPeersCql(ctx, r.GetConnOrTx(), params, cql)
	var peers []Peer
	var fullCount int64
	if err == nil {
		for _, r := range rows {
			fullCount = r.FullCount
			peers = append(peers, r.Peer)
		}
	}
	return peers, fullCount, err
}

func (r *PgIllRepo) GetLocatedSuppliersByIllTransactionAndStatus(ctx extctx.ExtendedContext, params GetLocatedSuppliersByIllTransactionAndStatusParams) ([]LocatedSupplier, error) {
	rows, err := r.queries.GetLocatedSuppliersByIllTransactionAndStatus(ctx, r.GetConnOrTx(), params)
	var suppliers []LocatedSupplier
	if err == nil {
		for _, r := range rows {
			suppliers = append(suppliers, r.LocatedSupplier)
		}
	}
	return suppliers, err
}

func (r *PgIllRepo) GetLocatedSuppliersByIllTransactionAndStatusForUpdate(ctx extctx.ExtendedContext, params GetLocatedSuppliersByIllTransactionAndStatusForUpdateParams) ([]LocatedSupplier, error) {
	rows, err := r.queries.GetLocatedSuppliersByIllTransactionAndStatusForUpdate(ctx, r.GetConnOrTx(), params)
	var suppliers []LocatedSupplier
	if err == nil {
		for _, r := range rows {
			suppliers = append(suppliers, r.LocatedSupplier)
		}
	}
	return suppliers, err
}

func (r *PgIllRepo) SavePeer(ctx extctx.ExtendedContext, params SavePeerParams) (Peer, error) {
	row, err := r.queries.SavePeer(ctx, r.GetConnOrTx(), params)
	return row.Peer, err
}

func (r *PgIllRepo) DeletePeer(ctx extctx.ExtendedContext, id string) error {
	return r.queries.DeletePeer(ctx, r.GetConnOrTx(), id)
}

func (r *PgIllRepo) SaveLocatedSupplier(ctx extctx.ExtendedContext, params SaveLocatedSupplierParams) (LocatedSupplier, error) {
	row, err := r.queries.SaveLocatedSupplier(ctx, r.GetConnOrTx(), params)
	return row.LocatedSupplier, err
}

func (r *PgIllRepo) GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams) (LocatedSupplier, error) {
	row, err := r.queries.GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx, r.GetConnOrTx(), params)
	return row.LocatedSupplier, err
}

func (r *PgIllRepo) GetLocatedSupplierByIllTransactionAndSymbol(ctx extctx.ExtendedContext, illTransId, symbol string) (LocatedSupplier, error) {
	row, err := r.queries.GetLocatedSupplierByIllTransactionAndSymbol(ctx, r.GetConnOrTx(), GetLocatedSupplierByIllTransactionAndSymbolParams{
		IllTransactionID: illTransId,
		SupplierSymbol:   symbol,
	})
	return row.LocatedSupplier, err
}

func (r *PgIllRepo) GetLocatedSuppliersByIllTransaction(ctx extctx.ExtendedContext, id string) ([]LocatedSupplier, int64, error) {
	rows, err := r.queries.GetLocatedSuppliersByIllTransaction(ctx, r.GetConnOrTx(), id)
	var suppliers []LocatedSupplier
	var fullCount int64
	if err == nil {
		for _, r := range rows {
			fullCount = r.FullCount
			suppliers = append(suppliers, r.LocatedSupplier)
		}
	}
	return suppliers, fullCount, err
}

func (r *PgIllRepo) GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (LocatedSupplier, error) {
	selSup, err := r.GetLocatedSuppliersByIllTransactionAndStatus(ctx, GetLocatedSuppliersByIllTransactionAndStatusParams{
		IllTransactionID: illTransId,
		SupplierStatus:   SupplierStateSelectedPg,
	})
	if err != nil {
		return LocatedSupplier{}, err
	}
	return getSelectedSupplierForIllTransactionForCommon(selSup, illTransId)
}

func (r *PgIllRepo) GetSelectedSupplierForIllTransactionForUpdate(ctx extctx.ExtendedContext, illTransId string) (LocatedSupplier, error) {
	selSup, err := r.GetLocatedSuppliersByIllTransactionAndStatusForUpdate(ctx, GetLocatedSuppliersByIllTransactionAndStatusForUpdateParams{
		IllTransactionID: illTransId,
		SupplierStatus:   SupplierStateSelectedPg,
	})
	if err != nil {
		return LocatedSupplier{}, err
	}
	return getSelectedSupplierForIllTransactionForCommon(selSup, illTransId)
}

func (r *PgIllRepo) DeleteLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error {
	return r.queries.DeleteLocatedSuppliersByIllTransaction(ctx, r.GetConnOrTx(), illTransId)
}

func (r *PgIllRepo) SaveSymbol(ctx extctx.ExtendedContext, params SaveSymbolParams) (Symbol, error) {
	sym, err := r.queries.SaveSymbol(ctx, r.GetConnOrTx(), params)
	return sym.Symbol, err
}

func (r *PgIllRepo) SaveBranchSymbol(ctx extctx.ExtendedContext, params SaveBranchSymbolParams) (BranchSymbol, error) {
	sym, err := r.queries.SaveBranchSymbol(ctx, r.GetConnOrTx(), params)
	return sym.BranchSymbol, err
}

func (r *PgIllRepo) GetSymbolsByPeerId(ctx extctx.ExtendedContext, peerId string) ([]Symbol, error) {
	rows, err := r.queries.GetSymbolsByPeerId(ctx, r.GetConnOrTx(), peerId)
	var symbols []Symbol
	if err == nil {
		for _, r := range rows {
			symbols = append(symbols, r.Symbol)
		}
	}
	return symbols, err
}

func (r *PgIllRepo) GetBranchSymbolsByPeerId(ctx extctx.ExtendedContext, peerId string) ([]BranchSymbol, error) {
	rows, err := r.queries.GetBranchSymbolsByPeerId(ctx, r.GetConnOrTx(), peerId)
	var symbols []BranchSymbol
	if err == nil {
		for _, r := range rows {
			symbols = append(symbols, r.BranchSymbol)
		}
	}
	return symbols, err
}

func (r *PgIllRepo) DeleteSymbolByPeerId(ctx extctx.ExtendedContext, peerId string) error {
	return r.queries.DeleteSymbolByPeerId(ctx, r.GetConnOrTx(), peerId)
}

func (r *PgIllRepo) DeleteBranchSymbolByPeerId(ctx extctx.ExtendedContext, peerId string) error {
	return r.queries.DeleteBranchSymbolsByPeerId(ctx, r.GetConnOrTx(), peerId)
}

func (r *PgIllRepo) GetIllTransactionByRequesterId(ctx extctx.ExtendedContext, peerId pgtype.Text) ([]IllTransaction, error) {
	rows, err := r.queries.GetIllTransactionByRequesterId(ctx, r.GetConnOrTx(), peerId)
	var trans []IllTransaction
	if err == nil {
		for _, r := range rows {
			trans = append(trans, r.IllTransaction)
		}
	}
	return trans, err
}

func (r *PgIllRepo) GetLocatedSupplierByPeerId(ctx extctx.ExtendedContext, peerId string) ([]LocatedSupplier, error) {
	rows, err := r.queries.GetLocatedSupplierByPeerId(ctx, r.GetConnOrTx(), peerId)
	var suppliers []LocatedSupplier
	if err == nil {
		for _, r := range rows {
			suppliers = append(suppliers, r.LocatedSupplier)
		}
	}
	return suppliers, err
}

func getSelectedSupplierForIllTransactionForCommon(selSup []LocatedSupplier, illTransId string) (LocatedSupplier, error) {
	if len(selSup) == 1 {
		return selSup[0], nil
	} else if len(selSup) == 0 {
		return LocatedSupplier{}, fmt.Errorf("did not find selected supplier for ILL transaction '%s': %w", illTransId, pgx.ErrNoRows)
	} else {
		return LocatedSupplier{}, fmt.Errorf("too many selected suppliers found for ILL transaction '%s': %w", illTransId, pgx.ErrTooManyRows)
	}
}

func (r *PgIllRepo) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, lookupSymbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]Peer, string, error) {
	symbolToPeer, symbolsToFetch := r.mapSymbolsAndFilterStale(ctx, lookupSymbols)
	if len(symbolsToFetch) == 0 {
		return getSliceFromMapInOrder(symbolToPeer, lookupSymbols), "<cached>", nil
	}
	dirEntries, err, query := directoryAdapter.Lookup(adapter.DirectoryLookupParams{
		Symbols: symbolsToFetch,
	})
	if err != nil {
		ctx.Logger().Warn("failed to lookup directory, non-cached symbols will be ignored", "symbols", symbolsToFetch, "error", err)
		return getSliceFromMapInOrder(symbolToPeer, lookupSymbols), query, err
	}
	if len(dirEntries) == 0 {
		ctx.Logger().Warn("empty directory response, non-cached symbols will be ignored", "symbols", symbolsToFetch)
		return getSliceFromMapInOrder(symbolToPeer, lookupSymbols), query, fmt.Errorf("empty directory response for symbols: %v", symbolsToFetch)
	}
	// directory lookup might return additional entries (e.g branches) or entries with new, duplicate or missing symbols
	// we iterate over all entries to eagerly refresh local data but ensure that the result list depends on the lookupSymbols
	for _, dirEntry := range dirEntries {
		if len(dirEntry.Symbols) == 0 {
			continue
		}
		var peer Peer
		for _, sym := range dirEntry.Symbols {
			peer, err = r.GetPeerBySymbol(ctx, sym)
			if err == nil { //peer found by one of the dir symbols, we can stop searching
				break
			}
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
		}
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) { //unlikely DB error, skip the entry
				ctx.Logger().Warn("failure when reading peer, symbols will be ignored", "symbols", dirEntry.Symbols, "error", err)
				continue
			}
		}
		if err == nil {
			// peer found locally, must have been stale, update it
			peer, _ = r.updateExistingPeer(ctx, peer, dirEntry)
		} else {
			// no local peer found, create a new one
			peer, _ = r.createNewPeer(ctx, dirEntry)
		}
		// see if the peer was in the refresh list, if so add it to the result
		for _, sym := range dirEntry.Symbols {
			if slices.Contains(symbolsToFetch, sym) {
				symbolToPeer[sym] = peer
				break
			}
		}
	}
	return getSliceFromMapInOrder(symbolToPeer, lookupSymbols), query, nil
}

func (r *PgIllRepo) createNewPeer(ctx extctx.ExtendedContext, dirEntry adapter.DirectoryEntry) (Peer, error) {
	var peer Peer
	var err error
	err = r.WithTxFunc(ctx, func(illRepo IllRepo) error {
		peer, err = illRepo.SavePeer(ctx, SavePeerParams{
			ID:            uuid.New().String(),
			Url:           dirEntry.URL,
			Name:          dirEntry.Name,
			RefreshPolicy: RefreshPolicyTransaction,
			RefreshTime:   GetPgNow(),
			Vendor:        string(dirEntry.Vendor),
			CustomData:    dirEntry.CustomData,
			BrokerMode:    string(dirEntry.BrokerMode),
		})
		if err != nil {
			ctx.Logger().Warn("could not save peer", "peerId", peer.ID, "symbols", dirEntry.Symbols, "error", err)
			return err
		}
		for _, sym := range dirEntry.Symbols {
			_, err = illRepo.SaveSymbol(ctx, SaveSymbolParams{
				SymbolValue: sym,
				PeerID:      peer.ID,
			})
			if err != nil {
				ctx.Logger().Warn("could not save symbol for peer", "peerId", peer.ID, "symbol", sym, "error", err)
				break
			}
		}
		for _, sym := range dirEntry.BranchSymbols {
			_, err = illRepo.SaveBranchSymbol(ctx, SaveBranchSymbolParams{
				SymbolValue: sym,
				PeerID:      peer.ID,
			})
			if err != nil {
				ctx.Logger().Warn("could not save branch symbol for peer", "peerId", peer.ID, "symbol", sym, "error", err)
				break
			}
		}
		return err
	})
	return peer, err
}

func (r *PgIllRepo) updateExistingPeer(ctx extctx.ExtendedContext, peer Peer, dirEntry adapter.DirectoryEntry) (Peer, error) {
	peer.Url = dirEntry.URL
	peer.CustomData = dirEntry.CustomData
	peer.Name = dirEntry.Name
	peer.Vendor = string(dirEntry.Vendor)
	peer.BrokerMode = string(dirEntry.BrokerMode)
	peer.RefreshTime = GetPgNow()
	peer, err := r.SavePeer(ctx, SavePeerParams(peer))
	if err != nil {
		ctx.Logger().Warn("could not update peer", "peerId", peer.ID, "symbols", dirEntry.Symbols, "error", err)
		return peer, err
	}
	err = r.DeleteSymbolByPeerId(ctx, peer.ID)
	if err != nil {
		ctx.Logger().Warn("could not delete peer symbols", "peerId", peer.ID, "symbols", dirEntry.Symbols, "error", err)
		return peer, err
	}
	for _, s := range dirEntry.Symbols {
		_, err = r.SaveSymbol(ctx, SaveSymbolParams{
			SymbolValue: s,
			PeerID:      peer.ID,
		})
		if err != nil {
			ctx.Logger().Warn("could not save peer symbol", "peerId", peer.ID, "symbol", s, "error", err)
			return peer, err
		}
	}
	err = r.DeleteBranchSymbolByPeerId(ctx, peer.ID)
	if err != nil {
		ctx.Logger().Warn("could not delete peer branch symbols", "peerId", peer.ID, "symbols", dirEntry.BranchSymbols, "error", err)
		return peer, err
	}
	for _, s := range dirEntry.BranchSymbols {
		_, err = r.SaveBranchSymbol(ctx, SaveBranchSymbolParams{
			SymbolValue: s,
			PeerID:      peer.ID,
		})
		if err != nil {
			ctx.Logger().Warn("could not save peer branch symbol", "peerId", peer.ID, "symbol", s, "error", err)
			return peer, err
		}
	}
	return peer, nil
}

func (r *PgIllRepo) mapSymbolsAndFilterStale(ctx extctx.ExtendedContext, symbols []string) (map[string]Peer, []string) {
	var symbolsToFetch []string
	symbolToPeer := make(map[string]Peer)
	for _, sym := range symbols {
		peer, err := r.GetPeerBySymbol(ctx, sym)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				symbolsToFetch = append(symbolsToFetch, sym)
			} else {
				ctx.Logger().Error("failed to read peer", "symbol", sym, "error", err)
			}
		} else {
			if peer.RefreshPolicy == RefreshPolicyTransaction && time.Now().UTC().After(peer.RefreshTime.Time.Add(PeerRefreshInterval)) {
				symbolsToFetch = append(symbolsToFetch, sym)
			} else {
				symbolToPeer[sym] = peer
			}
		}
	}
	return symbolToPeer, symbolsToFetch
}

func getSliceFromMapInOrder(symbolToPeer map[string]Peer, symbols []string) []Peer {
	peers := make([]Peer, 0, len(symbolToPeer))
	// first add peers that match the original symbols
	for _, sym := range symbols {
		if peer, ok := symbolToPeer[sym]; ok {
			peers = append(peers, peer)
			// remove to avoid duplicate lookup (holding) symbols
			delete(symbolToPeer, sym)
		}
	}
	return peers
}

func GetPgNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now().UTC(),
		Valid: true,
	}
}
