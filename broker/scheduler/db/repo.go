package sched_db

import (
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SchedulerChannel = "crosslink_sched_channel"

type SchedRepo interface {
	repo.Transactional[SchedRepo]
	SaveScheduledTask(ctx common.ExtendedContext, params SaveScheduledTaskParams) (ScheduledTask, error)
	ClaimNextScheduledTask(ctx common.ExtendedContext) (ScheduledTask, error)
	GetNextRunAt(ctx common.ExtendedContext) (pgtype.Timestamptz, error)
	GetStuckRunningTasks(ctx common.ExtendedContext, stuckAfter time.Duration) ([]ScheduledTask, error)
	GetScheduledTaskById(ctx common.ExtendedContext, id string) (ScheduledTask, error)

	// Batch action methods.
	SaveBatchAction(ctx common.ExtendedContext, params SaveBatchActionParams) (BatchAction, error)
	GetBatchActionById(ctx common.ExtendedContext, id, owner string) (BatchAction, error)
	DeleteBatchAction(ctx common.ExtendedContext, id, owner string) error
	GetBatchActions(ctx common.ExtendedContext, params GetBatchActionsParams) ([]BatchAction, int64, error)
}

type PgSchedRepo struct {
	repo.PgBaseRepo[SchedRepo]
	queries Queries
}

// WithTxFunc delegates transaction handling to PgBaseRepo.
func (r *PgSchedRepo) WithTxFunc(ctx common.ExtendedContext, fn func(SchedRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// CreateWithPgBaseRepo creates a derived repo bound to the provided tx-aware base.
func (r *PgSchedRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[SchedRepo]) SchedRepo {
	derived := new(PgSchedRepo)
	derived.PgBaseRepo = *base
	return derived
}

// CreateSchedRepo creates a new SchedRepo backed by the given connection pool.
func CreateSchedRepo(dbPool *pgxpool.Pool) SchedRepo {
	r := new(PgSchedRepo)
	r.Pool = dbPool
	return r
}

func (r *PgSchedRepo) SaveScheduledTask(ctx common.ExtendedContext, params SaveScheduledTaskParams) (ScheduledTask, error) {
	row, err := r.queries.SaveScheduledTask(ctx, r.GetConnOrTx(), params)
	if err == nil {
		r.notify(ctx)
	}
	return row.ScheduledTask, err
}

func (r *PgSchedRepo) ClaimNextScheduledTask(ctx common.ExtendedContext) (ScheduledTask, error) {
	row, err := r.queries.ClaimNextScheduledTask(ctx, r.GetConnOrTx())
	return row.ScheduledTask, err
}

func (r *PgSchedRepo) GetNextRunAt(ctx common.ExtendedContext) (pgtype.Timestamptz, error) {
	return r.queries.GetNextRunAt(ctx, r.GetConnOrTx())
}

// GetStuckRunningTasks returns tasks that have been in 'running' state for
// longer than stuckAfter, indicating they were claimed but never completed.
func (r *PgSchedRepo) GetStuckRunningTasks(ctx common.ExtendedContext, stuckAfter time.Duration) ([]ScheduledTask, error) {
	rows, err := r.queries.GetStuckRunningTasks(ctx, r.GetConnOrTx(), pgDuration(stuckAfter))
	if err != nil {
		return nil, err
	}
	tasks := make([]ScheduledTask, 0, len(rows))
	for _, row := range rows {
		tasks = append(tasks, row.ScheduledTask)
	}
	return tasks, nil
}

func (r *PgSchedRepo) GetScheduledTaskById(ctx common.ExtendedContext, id string) (ScheduledTask, error) {
	row, err := r.queries.GetScheduledTaskById(ctx, r.GetConnOrTx(), id)
	return row.ScheduledTask, err
}

func pgDuration(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

func (r *PgSchedRepo) notify(ctx common.ExtendedContext) {
	_, err := r.GetConnOrTx().Exec(ctx, "NOTIFY "+SchedulerChannel)
	if err != nil {
		ctx.Logger().Error("failed to notify scheduler channel", "channel", SchedulerChannel, "error", err)
	}
}

func (r *PgSchedRepo) SaveBatchAction(ctx common.ExtendedContext, params SaveBatchActionParams) (BatchAction, error) {
	row, err := r.queries.SaveBatchAction(ctx, r.GetConnOrTx(), params)
	return row.BatchAction, err
}

func (r *PgSchedRepo) GetBatchActionById(ctx common.ExtendedContext, id, owner string) (BatchAction, error) {
	row, err := r.queries.GetBatchActionById(ctx, r.GetConnOrTx(), GetBatchActionByIdParams{
		ID:    id,
		Owner: owner,
	})
	return row.BatchAction, err
}

func (r *PgSchedRepo) DeleteBatchAction(ctx common.ExtendedContext, id, owner string) error {
	return r.queries.DeleteBatchAction(ctx, r.GetConnOrTx(), DeleteBatchActionParams{
		ID:    id,
		Owner: owner,
	})
}

func (r *PgSchedRepo) GetBatchActions(ctx common.ExtendedContext, params GetBatchActionsParams) ([]BatchAction, int64, error) {
	rows, err := r.queries.GetBatchActions(ctx, r.GetConnOrTx(), params)
	var actions []BatchAction
	var fullCount int64
	if err == nil {
		if len(rows) > 0 {
			fullCount = rows[0].FullCount
			for _, r := range rows {
				fullCount = r.FullCount
				actions = append(actions, r.BatchAction)
			}
		} else {
			params.Limit = 1
			params.Offset = 0
			rows, err := r.queries.GetBatchActions(ctx, r.GetConnOrTx(), params)
			if err == nil && len(rows) > 0 {
				fullCount = rows[0].FullCount
			}
		}
	}
	return actions, fullCount, err
}
