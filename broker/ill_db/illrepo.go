package ill_db

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type IllRepo interface {
	SaveIllTransaction(ctx extctx.ExtendedContext, params SaveIllTransactionParams) (IllTransaction, error)
	GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error)
	GetIllTransactionById(ctx extctx.ExtendedContext, id string) (IllTransaction, error)
	ListIllTransactions(ctx extctx.ExtendedContext) ([]IllTransaction, error)
	SavePeer(ctx extctx.ExtendedContext, params SavePeerParams) (Peer, error)
	GetPeerById(ctx extctx.ExtendedContext, id string) (Peer, error)
	GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (Peer, error)
	ListPeers(ctx extctx.ExtendedContext) ([]Peer, error)
	DeletePeer(ctx extctx.ExtendedContext, id string) error
	SaveLocatedSupplier(ctx extctx.ExtendedContext, params SaveLocatedSupplierParams) (LocatedSupplier, error)
	GetLocatedSupplierByIllTransactionAndStatus(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndStatusParams) ([]LocatedSupplier, error)
	GetLocatedSupplierByIllTransactionAndSupplier(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndSupplierParams) (LocatedSupplier, error)
	GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (LocatedSupplier, error)
	GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) []Peer
}

type PgIllRepo struct {
	repo.PgBaseRepo[IllRepo]
	queries Queries
}

func (r *PgIllRepo) SaveIllTransaction(ctx extctx.ExtendedContext, params SaveIllTransactionParams) (IllTransaction, error) {
	row, err := r.queries.SaveIllTransaction(ctx, r.GetConnOrTx(), params)
	return row.IllTransaction, err
}

func (r *PgIllRepo) GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error) {
	row, err := r.queries.GetIllTransactionByRequesterRequestId(ctx, r.GetConnOrTx(), requesterRequestID)
	return row.IllTransaction, err
}

func (r *PgIllRepo) GetIllTransactionById(ctx extctx.ExtendedContext, id string) (IllTransaction, error) {
	row, err := r.queries.GetIllTransactionById(ctx, r.GetConnOrTx(), id)
	return row.IllTransaction, err
}

func (r *PgIllRepo) ListIllTransactions(ctx extctx.ExtendedContext) ([]IllTransaction, error) {
	rows, err := r.queries.ListIllTransactions(ctx, r.GetConnOrTx())
	var transactions []IllTransaction
	if err == nil {
		for _, r := range rows {
			transactions = append(transactions, r.IllTransaction)
		}
	}
	return transactions, err
}

func (r *PgIllRepo) GetPeerById(ctx extctx.ExtendedContext, id string) (Peer, error) {
	row, err := r.queries.GetPeerById(ctx, r.GetConnOrTx(), id)
	return row.Peer, err
}

func (r *PgIllRepo) GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (Peer, error) {
	row, err := r.queries.GetPeerBySymbol(ctx, r.GetConnOrTx(), symbol)
	return row.Peer, err
}

func (r *PgIllRepo) ListPeers(ctx extctx.ExtendedContext) ([]Peer, error) {
	rows, err := r.queries.ListPeers(ctx, r.GetConnOrTx())
	var peers []Peer
	if err == nil {
		for _, r := range rows {
			peers = append(peers, r.Peer)
		}
	}
	return peers, err
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

func (r *PgIllRepo) GetLocatedSupplierByIllTransactionAndSupplier(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransactionAndSupplierParams) (LocatedSupplier, error) {
	row, err := r.queries.GetLocatedSupplierByIllTransactionAndSupplier(ctx, r.GetConnOrTx(), params)
	return row.LocatedSupplier, err
}

func (r *PgIllRepo) GetSelectedSupplierForIllTransaction(ctx extctx.ExtendedContext, illTransId string) (LocatedSupplier, error) {
	selSup, err := r.GetLocatedSupplierByIllTransactionAndStatus(ctx, GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: illTransId,
		SupplierStatus:   SupplierStatusSelectedPg,
	})
	if err != nil {
		return LocatedSupplier{}, err
	}
	if len(selSup) == 1 {
		return selSup[0], err
	} else if len(selSup) == 0 {
		return LocatedSupplier{}, errors.New("did not find selected supplier for ILL transaction: " + illTransId)
	} else {
		return LocatedSupplier{}, errors.New("too many selected suppliers found for ILL transaction: " + illTransId)
	}
}

func (r *PgIllRepo) GetCachedPeersBySymbols(ctx extctx.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) []Peer {
	var peers []Peer
	if len(symbols) > 0 {
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
					peers = append(peers, peer)
				}
			}
		}
		if len(symbolsToFetch) > 0 {
			dirEntries, err := directoryAdapter.Lookup(adapter.DirectoryLookupParams{
				Symbols: symbolsToFetch,
			})
			if err != nil {
				ctx.Logger().Error("failed to get dirEntries by symbols", "symbols", symbolsToFetch, "error", err)
			} else if len(dirEntries) == 0 {
				ctx.Logger().Error("did not find dirEntries by symbols", "symbols", symbolsToFetch, "error", err)
			} else {
				for _, dir := range dirEntries {
					peer, loopErr := r.GetPeerBySymbol(ctx, dir.Symbol)
					if loopErr != nil {
						if errors.Is(loopErr, pgx.ErrNoRows) {
							peer, err = r.SavePeer(ctx, SavePeerParams{
								ID:            uuid.New().String(),
								Symbol:        dir.Symbol,
								Url:           dir.URL,
								Name:          dir.Symbol,
								RefreshPolicy: RefreshPolicyTransaction,
								RefreshTime:   GetPgNow(),
								Vendor:        dir.Vendor,
							})
							if err != nil {
								ctx.Logger().Error("failed to save peer", "symbol", dir.Symbol, "error", err)
							} else {
								peers = append(peers, peer)
							}
						} else {
							ctx.Logger().Error("failed to read peer", "symbol", dir.Symbol, "error", err)
						}
					} else {
						peer.Url = dir.URL
						peer.RefreshTime = GetPgNow()
						peer, err = r.SavePeer(ctx, SavePeerParams(peer))
						if err != nil {
							ctx.Logger().Error("could not update peer", "symbol", dir.Symbol, "error", err)
						} else {
							peers = append(peers, peer)
						}
					}
				}
			}
		}
	}
	return peers
}

func GetPgNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
