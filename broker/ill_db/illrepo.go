package ill_db

import (
	"context"

	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5/pgtype"
)

type IllRepo interface {
	repo.BaseRepo[IllRepo]
	CreateIllTransaction(params CreateIllTransactionParams) (IllTransaction, error)
	GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (IllTransaction, error)
}

type PgIllRepo struct {
	repo.PgBaseRepo[IllRepo]
	queries Queries
}

func (r *PgIllRepo) CreateIllTransaction(params CreateIllTransactionParams) (IllTransaction, error) {
	row, err := r.queries.CreateIllTransaction(context.Background(), r.GetPoolOrTx(), params)
	return row.IllTransaction, err
}

func (r *PgIllRepo) GetIllTransactionByRequesterRequestId(requesterRequestID pgtype.Text) (IllTransaction, error) {
	row, err := r.queries.GetIllTransactionByRequesterRequestId(context.Background(), r.GetPoolOrTx(), requesterRequestID)
	return row.IllTransaction, err
}
