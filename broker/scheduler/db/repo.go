package skd_db

import (
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schedulerChannel = "scheduler_channel"

type SkdRepo interface {
	repo.Transactional[SkdRepo]
	SaveScheduledTask(ctx common.ExtendedContext, params SaveScheduledTaskParams) (ScheduledTask, error)
	ClaimNextScheduledTask(ctx common.ExtendedContext) (ScheduledTask, error)
	GetNextRunAt(ctx common.ExtendedContext) (pgtype.Timestamptz, error)
}

type PgSkdRepo struct {
	repo.PgBaseRepo[SkdRepo]
	queries Queries
}

// WithTxFunc delegates transaction handling to PgBaseRepo.
func (r *PgSkdRepo) WithTxFunc(ctx common.ExtendedContext, fn func(SkdRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// CreateWithPgBaseRepo creates a derived repo bound to the provided tx-aware base.
func (r *PgSkdRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[SkdRepo]) SkdRepo {
	derived := new(PgSkdRepo)
	derived.PgBaseRepo = *base
	return derived
}

// CreateSkdRepo creates a new SkdRepo backed by the given connection pool.
func CreateSkdRepo(dbPool *pgxpool.Pool) SkdRepo {
	r := new(PgSkdRepo)
	r.Pool = dbPool
	return r
}

func (r *PgSkdRepo) SaveScheduledTask(ctx common.ExtendedContext, params SaveScheduledTaskParams) (ScheduledTask, error) {
	row, err := r.queries.SaveScheduledTask(ctx, r.GetConnOrTx(), params)
	if err == nil {
		r.notify(ctx)
	}
	return row.ScheduledTask, err
}

func (r *PgSkdRepo) ClaimNextScheduledTask(ctx common.ExtendedContext) (ScheduledTask, error) {
	row, err := r.queries.ClaimNextScheduledTask(ctx, r.GetConnOrTx())
	return row.ScheduledTask, err
}

func (r *PgSkdRepo) GetNextRunAt(ctx common.ExtendedContext) (pgtype.Timestamptz, error) {
	return r.queries.GetNextRunAt(ctx, r.GetConnOrTx())
}

func (r *PgSkdRepo) notify(ctx common.ExtendedContext) {
	_, err := r.GetConnOrTx().Exec(ctx, "NOTIFY "+schedulerChannel)
	if err != nil {
		ctx.Logger().Error("failed to notify scheduler channel", "channel", schedulerChannel, "error", err)
	}
}
