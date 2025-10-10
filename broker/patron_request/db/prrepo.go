package pr_db

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
)

type PrRepo interface {
	repo.Transactional[PrRepo]
	GetPatronRequestById(ctx common.ExtendedContext, id string) (PatronRequest, error)
	ListPatronRequests(ctx common.ExtendedContext) ([]PatronRequest, error)
	SavePatronRequest(ctx common.ExtendedContext, params SavePatronRequestParams) (PatronRequest, error)
	DeletePatronRequest(ctx common.ExtendedContext, id string) error
}

type PgPrRepo struct {
	repo.PgBaseRepo[PrRepo]
	queries Queries
}

// delegate transaction handling to Base
func (r *PgPrRepo) WithTxFunc(ctx common.ExtendedContext, fn func(PrRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// DerivedRepo
func (r *PgPrRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[PrRepo]) PrRepo {
	prRepo := new(PgPrRepo)
	prRepo.PgBaseRepo = *base
	return prRepo
}

func (r *PgPrRepo) GetPatronRequestById(ctx common.ExtendedContext, id string) (PatronRequest, error) {
	row, err := r.queries.GetPatronRequestById(ctx, r.GetConnOrTx(), id)
	return row.PatronRequest, err
}

func (r *PgPrRepo) ListPatronRequests(ctx common.ExtendedContext) ([]PatronRequest, error) {
	rows, err := r.queries.ListPatronRequests(ctx, r.GetConnOrTx())
	var list []PatronRequest
	if err == nil {
		for _, r := range rows {
			list = append(list, r.PatronRequest)
		}
	}
	return list, err
}

func (r *PgPrRepo) SavePatronRequest(ctx common.ExtendedContext, params SavePatronRequestParams) (PatronRequest, error) {
	row, err := r.queries.SavePatronRequest(ctx, r.GetConnOrTx(), params)
	return row.PatronRequest, err
}

func (r *PgPrRepo) DeletePatronRequest(ctx common.ExtendedContext, id string) error {
	return r.queries.DeletePatronRequest(ctx, r.GetConnOrTx(), id)
}
