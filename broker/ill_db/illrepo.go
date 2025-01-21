package ill_db

import (
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5/pgtype"
)

type IllRepo interface {
	CreateIllTransaction(ctx extctx.ExtendedContext, params CreateIllTransactionParams) (IllTransaction, error)
	GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error)
	GetIllTransactionById(ctx extctx.ExtendedContext, id string) (IllTransaction, error)
	CreatePeer(ctx extctx.ExtendedContext, params CreatePeerParams) (Peer, error)
	GetPeerById(ctx extctx.ExtendedContext, id string) (Peer, error)
	GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (Peer, error)
	CreateLocatedSupplier(ctx extctx.ExtendedContext, params CreateLocatedSupplierParams) (LocatedSupplier, error)
	GetLocatedSupplierByIllTransitionAndStatus(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransitionAndStatusParams) ([]LocatedSupplier, error)
}

type PgIllRepo struct {
	repo.PgBaseRepo[IllRepo]
	queries Queries
}

func (r *PgIllRepo) CreateIllTransaction(ctx extctx.ExtendedContext, params CreateIllTransactionParams) (IllTransaction, error) {
	row, err := r.queries.CreateIllTransaction(ctx, r.GetConnOrTx(), params)
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

func (r *PgIllRepo) GetPeerById(ctx extctx.ExtendedContext, id string) (Peer, error) {
	row, err := r.queries.GetPeerById(ctx, r.GetConnOrTx(), id)
	return row.Peer, err
}

func (r *PgIllRepo) GetPeerBySymbol(ctx extctx.ExtendedContext, symbol string) (Peer, error) {
	row, err := r.queries.GetPeerBySymbol(ctx, r.GetConnOrTx(), symbol)
	return row.Peer, err
}

func (r *PgIllRepo) GetLocatedSupplierByIllTransitionAndStatus(ctx extctx.ExtendedContext, params GetLocatedSupplierByIllTransitionAndStatusParams) ([]LocatedSupplier, error) {
	rows, err := r.queries.GetLocatedSupplierByIllTransitionAndStatus(ctx, r.GetConnOrTx(), params)
	var suppliers []LocatedSupplier
	if err == nil {
		for _, r := range rows {
			suppliers = append(suppliers, r.LocatedSupplier)
		}
	}
	return suppliers, err
}

func (r *PgIllRepo) CreatePeer(ctx extctx.ExtendedContext, params CreatePeerParams) (Peer, error) {
	row, err := r.queries.CreatePeer(ctx, r.GetConnOrTx(), params)
	return row.Peer, err
}

func (r *PgIllRepo) CreateLocatedSupplier(ctx extctx.ExtendedContext, params CreateLocatedSupplierParams) (LocatedSupplier, error) {
	row, err := r.queries.CreateLocatedSupplier(ctx, r.GetConnOrTx(), params)
	return row.LocatedSupplier, err
}
