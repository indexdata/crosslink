package ill_db

import (
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5/pgtype"
)

type IllRepo interface {
	CreateIllTransaction(ctx extctx.ExtendedContext, params CreateIllTransactionParams) (IllTransaction, error)
	GetIllTransactionByRequesterRequestId(ctx extctx.ExtendedContext, requesterRequestID pgtype.Text) (IllTransaction, error)
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
