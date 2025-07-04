package ill_db

import (
	"errors"
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
	GetLocatedSupplierByIllTransactionAndStatus(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndStatusParams) ([]LocatedSupplier, error)
	GetLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, id string) ([]LocatedSupplier, int64, error)
	GetLocatedSupplierByIllTransactionAndStatusForUpdate(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndStatusForUpdateParams) ([]LocatedSupplier, error)
	GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams) (LocatedSupplier, error)
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

func (r *PgIllRepo) GetLocatedSupplierByIllTransactionAndStatus(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndStatusParams) ([]LocatedSupplier, error) {
	rows, err := r.queries.GetLocatedSupplierByIllTransactionAndStatus(ctx, r.GetConnOrTx(), params)
	var suppliers []LocatedSupplier
	if err == nil {
		for _, r := range rows {
			suppliers = append(suppliers, r.LocatedSupplier)
		}
	}
	return suppliers, err
}

func (r *PgIllRepo) GetLocatedSupplierByIllTransactionAndStatusForUpdate(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndStatusForUpdateParams) ([]LocatedSupplier, error) {
	rows, err := r.queries.GetLocatedSupplierByIllTransactionAndStatusForUpdate(ctx, r.GetConnOrTx(), params)
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

func (r *PgIllRepo) GetLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, id string) ([]LocatedSupplier, int64, error) {
	rows, err := r.queries.GetLocatedSupplierByIllTransaction(ctx, r.GetConnOrTx(), id)
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
	selSup, err := r.GetLocatedSupplierByIllTransactionAndStatus(ctx, GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: illTransId,
		SupplierStatus:   SupplierStatusSelectedPg,
	})
	if err != nil {
		return LocatedSupplier{}, err
	}
	return getSelectedSupplierForIllTransactionForCommon(selSup, illTransId)
}

func (r *PgIllRepo) GetSelectedSupplierForIllTransactionForUpdate(ctx extctx.ExtendedContext, illTransId string) (LocatedSupplier, error) {
	selSup, err := r.GetLocatedSupplierByIllTransactionAndStatusForUpdate(ctx, GetLocatedSupplierByIllTransactionAndStatusForUpdateParams{
		IllTransactionID: illTransId,
		SupplierStatus:   SupplierStatusSelectedPg,
	})
	if err != nil {
		return LocatedSupplier{}, err
	}
	return getSelectedSupplierForIllTransactionForCommon(selSup, illTransId)
}

func (r *PgIllRepo) DeleteLocatedSupplierByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error {
	return r.queries.DeleteLocatedSupplierByIllTransaction(ctx, r.GetConnOrTx(), illTransId)
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
	return r.queries.DeleteBranchSymbolByPeerId(ctx, r.GetConnOrTx(), peerId)
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
		return LocatedSupplier{}, errors.New("did not find selected supplier for ILL transaction: " + illTransId)
	} else {
		return LocatedSupplier{}, errors.New("too many selected suppliers found for ILL transaction: " + illTransId)
	}
}

func symCheck(searchSymbols []string, foundSymbols []string) bool {
	for _, sym := range foundSymbols {
		if slices.Contains(searchSymbols, sym) {
			return true
		}
	}
	return false
}

func (r *PgIllRepo) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]Peer, string, error) {
	smap := make(map[string]string) // symbol to peer ID map
	peers := make(map[string]Peer)
	var query string
	symbolsToFetch := r.collectCurrentData(ctx, symbols, peers, smap)
	if len(symbolsToFetch) == 0 {
		return MapToPeerSlice(peers, smap, symbols), query, nil
	}
	dirEntries, err, queryVal := directoryAdapter.Lookup(adapter.DirectoryLookupParams{
		Symbols: symbolsToFetch,
	})
	query = queryVal
	if err != nil {
		ctx.Logger().Error("failed to get dirEntries by symbols", "symbols", symbolsToFetch, "error", err)
		return MapToPeerSlice(peers, smap, symbols), query, err
	}
	if len(dirEntries) == 0 {
		ctx.Logger().Error("did not find dirEntries by symbols", "symbols", symbolsToFetch, "error", err)
		return MapToPeerSlice(peers, smap, symbols), query, errors.New("did not find dirEntries by symbols")
	}
	for _, dir := range dirEntries {
		if !symCheck(symbols, dir.Symbols) {
			continue
		}
		if len(dir.Symbols) == 0 {
			continue
		}
		var peer Peer
		for _, sym := range dir.Symbols {
			peer, err = r.GetPeerBySymbol(ctx, sym)
			if err == nil {
				break
			}
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
		}
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				ctx.Logger().Error("failed to read peer", "symbol", dir.Symbols, "error", err)
				continue
			}
		}
		if err == nil {
			// cached peer found
			r.updateExistingPeer(ctx, peer, dir, peers, smap)
		} else {
			peer, err = r.createNewPeer(ctx, dir, smap)
			if err != nil {
				ctx.Logger().Error("failed to save peer", "symbol", dir.Symbols, "error", err)
			} else {
				peers[peer.ID] = peer
			}
		}
	}
	return MapToPeerSlice(peers, smap, symbols), query, nil
}

func (r *PgIllRepo) createNewPeer(ctx extctx.ExtendedContext, dir adapter.DirectoryEntry, smap map[string]string) (Peer, error) {
	var peer Peer
	var err error
	err = r.WithTxFunc(ctx, func(illRepo IllRepo) error {
		peer, err = illRepo.SavePeer(ctx, SavePeerParams{
			ID:            uuid.New().String(),
			Url:           dir.URL,
			Name:          dir.Name,
			RefreshPolicy: RefreshPolicyTransaction,
			RefreshTime:   GetPgNow(),
			Vendor:        string(dir.Vendor),
			CustomData:    dir.CustomData,
			BrokerMode:    string(dir.BrokerMode),
		})
		if err != nil {
			return err
		}
		for _, sym := range dir.Symbols {
			_, err = illRepo.SaveSymbol(ctx, SaveSymbolParams{
				SymbolValue: sym,
				PeerID:      peer.ID,
			})
			if err != nil {
				break
			}
			smap[sym] = peer.ID
		}
		for _, sym := range dir.BranchSymbols {
			_, err = illRepo.SaveBranchSymbol(ctx, SaveBranchSymbolParams{
				SymbolValue: sym,
				PeerID:      peer.ID,
			})
			if err != nil {
				break
			}
		}
		return err
	})
	return peer, err
}

func (r *PgIllRepo) updateExistingPeer(ctx extctx.ExtendedContext, peer Peer, dir adapter.DirectoryEntry, peers map[string]Peer, smap map[string]string) {
	var err error
	peer.Url = dir.URL
	peer.CustomData = dir.CustomData
	peer.Name = dir.Name
	peer.Vendor = string(dir.Vendor)
	peer.BrokerMode = string(dir.BrokerMode)
	peer.RefreshTime = GetPgNow()
	peer, err = r.SavePeer(ctx, SavePeerParams(peer))
	if err != nil {
		ctx.Logger().Error("could not update peer", "symbol", dir.Symbols, "error", err)
		return
	}
	err = r.DeleteSymbolByPeerId(ctx, peer.ID)
	if err != nil {
		ctx.Logger().Error("could not delete peer symbols", "symbols", dir.Symbols, "error", err)
		return
	}
	for _, s := range dir.Symbols {
		_, e := r.SaveSymbol(ctx, SaveSymbolParams{
			SymbolValue: s,
			PeerID:      peer.ID,
		})
		if e != nil {
			ctx.Logger().Error("could not save peer symbol", "symbol", s, "error", err)
			return
		}
		smap[s] = peer.ID
	}
	err = r.DeleteBranchSymbolByPeerId(ctx, peer.ID)
	if err != nil {
		ctx.Logger().Error("could not delete peer branch symbols", "symbols", dir.Symbols, "error", err)
		return
	}
	for _, s := range dir.BranchSymbols {
		_, e := r.SaveBranchSymbol(ctx, SaveBranchSymbolParams{
			SymbolValue: s,
			PeerID:      peer.ID,
		})
		if e != nil {
			ctx.Logger().Error("could not save peer branch symbol", "symbol", s, "error", err)
			return
		}
	}
	peers[peer.ID] = peer
}

func (r *PgIllRepo) collectCurrentData(ctx extctx.ExtendedContext, symbols []string, peers map[string]Peer, smap map[string]string) []string {
	var symbolsToFetch []string
	for _, sym := range symbols {
		peer, err := r.GetPeerBySymbol(ctx, sym)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				symbolsToFetch = append(symbolsToFetch, sym)
			} else {
				ctx.Logger().Error("failed to read peer", "symbol", sym, "error", err)
			}
		} else {
			if peer.RefreshPolicy == RefreshPolicyTransaction && time.Now().After(peer.RefreshTime.Time.Add(PeerRefreshInterval)) {
				symbolsToFetch = append(symbolsToFetch, sym)
			} else {
				peers[peer.ID] = peer
				smap[sym] = peer.ID
			}
		}
	}
	return symbolsToFetch
}

func MapToPeerSlice(m map[string]Peer, smap map[string]string, symbols []string) []Peer {
	peers := make([]Peer, 0, len(m))
	// first add peers that match the original symbols
	for _, sym := range symbols {
		if peerId, ok := smap[sym]; ok {
			if peer, ok := m[peerId]; ok {
				peers = append(peers, peer)
				// remove from m to avoid duplicates
				delete(m, peerId)
			}
		}
	}
	// then add remaining peers
	for _, peer := range m {
		peers = append(peers, peer)
	}
	return peers
}
func GetPgNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
