package ps_db

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PsRepo defines pull slip persistence operations.
type PsRepo interface {
	repo.Transactional[PsRepo]
	SavePullSlip(ctx common.ExtendedContext, params SavePullSlipParams) (PullSlip, error)
	GetPullSlipByIdAndOwner(ctx common.ExtendedContext, id string, owner string) (PullSlip, error)
}

type PgPsRepo struct {
	repo.PgBaseRepo[PsRepo]
	queries Queries
}

// WithTxFunc delegates transaction handling to PgBaseRepo.
func (r *PgPsRepo) WithTxFunc(ctx common.ExtendedContext, fn func(PsRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// CreateWithPgBaseRepo creates a derived repo bound to the provided tx-aware base.
func (r *PgPsRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[PsRepo]) PsRepo {
	derived := new(PgPsRepo)
	derived.PgBaseRepo = *base
	return derived
}

// CreatePsRepo creates a new PsRepo backed by the given connection pool.
func CreatePsRepo(dbPool *pgxpool.Pool) PsRepo {
	r := new(PgPsRepo)
	r.Pool = dbPool
	return r
}

func (r *PgPsRepo) SavePullSlip(ctx common.ExtendedContext, params SavePullSlipParams) (PullSlip, error) {
	row, err := r.queries.SavePullSlip(ctx, r.GetConnOrTx(), params)
	return row.PullSlip, err
}

func (r *PgPsRepo) GetPullSlipByIdAndOwner(ctx common.ExtendedContext, id string, owner string) (PullSlip, error) {
	row, err := r.queries.GetPullSlipByIdAndOwner(ctx, r.GetConnOrTx(), GetPullSlipByIdAndOwnerParams{ID: id, Owner: owner})
	return row.PullSlip, err
}
