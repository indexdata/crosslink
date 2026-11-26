package sched_db

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const SchedulerChannel = "crosslink_sched_channel"

// ErrBatchActionRunActive means the owner already has an unfinished run of this action.
var ErrBatchActionRunActive = errors.New("a run for this owner and action is already active")

// BatchActionRunActiveError identifies the run that prevents another dispatch.
type BatchActionRunActiveError struct {
	ID        string
	StartedAt time.Time
}

func (e *BatchActionRunActiveError) Error() string {
	return fmt.Sprintf("%s (run %s, started %s)", ErrBatchActionRunActive, e.ID, e.StartedAt.Format(time.RFC3339Nano))
}

func (e *BatchActionRunActiveError) Unwrap() error {
	return ErrBatchActionRunActive
}

type SchedRepo interface {
	QueueBatchActionRun(common.ExtendedContext, ScheduledTask) error
	repo.Transactional[SchedRepo]
	SaveScheduledTask(ctx common.ExtendedContext, params SaveScheduledTaskParams) (ScheduledTask, error)
	ClaimNextScheduledTask(ctx common.ExtendedContext) (ScheduledTask, error)
	GetNextRunAt(ctx common.ExtendedContext) (pgtype.Timestamptz, error)
	GetStuckRunningTasks(ctx common.ExtendedContext, stuckAfter time.Duration) ([]ScheduledTask, error)
	GetScheduledTaskById(ctx common.ExtendedContext, id string, owners []string) (ScheduledTask, error)
	GetScheduledTaskByIdForUpdate(ctx common.ExtendedContext, id string, owners []string) (ScheduledTask, error)
	HasActiveBatchActionEvents(ctx common.ExtendedContext, taskID string) (bool, error)
	DeleteBatchActionEvents(ctx common.ExtendedContext, taskID string) error
	DeleteOldBatchActionRunEvents(ctx common.ExtendedContext, currentEventId string, taskID string, retention int32) error
	DeleteScheduledTask(ctx common.ExtendedContext, id string, owners []string) error
	GetScheduledTasks(ctx common.ExtendedContext, params GetScheduledTasksParams) ([]ScheduledTask, int64, error)
}

type PgSchedRepo struct {
	repo.PgBaseRepo[SchedRepo]
	queries      Queries
	eventQueries events.Queries
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
	if !params.UpdatedAt.Valid {
		params.UpdatedAt = params.CreatedAt
	}
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

func pgDuration(d time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: d.Microseconds(), Valid: true}
}

func (r *PgSchedRepo) notify(ctx common.ExtendedContext) {
	_, err := r.GetConnOrTx().Exec(ctx, "NOTIFY "+SchedulerChannel)
	if err != nil {
		ctx.Logger().Error("failed to notify scheduler channel", "channel", SchedulerChannel, "error", err)
	}
}

func (r *PgSchedRepo) GetScheduledTaskById(ctx common.ExtendedContext, id string, owners []string) (ScheduledTask, error) {
	row, err := r.queries.GetScheduledTaskById(ctx, r.GetConnOrTx(), GetScheduledTaskByIdParams{
		ID:     id,
		Owners: owners,
	})
	return row.ScheduledTask, err
}

func (r *PgSchedRepo) GetScheduledTaskByIdForUpdate(ctx common.ExtendedContext, id string, owners []string) (ScheduledTask, error) {
	row, err := r.queries.GetScheduledTaskByIdForUpdate(ctx, r.GetConnOrTx(), GetScheduledTaskByIdForUpdateParams{
		ID:     id,
		Owners: owners,
	})
	return row.ScheduledTask, err
}

func (r *PgSchedRepo) DeleteScheduledTask(ctx common.ExtendedContext, id string, owners []string) error {
	return r.queries.DeleteScheduledTask(ctx, r.GetConnOrTx(), DeleteScheduledTaskParams{
		ID:     id,
		Owners: owners,
	})
}

func (r *PgSchedRepo) HasActiveBatchActionEvents(ctx common.ExtendedContext, taskID string) (bool, error) {
	return r.eventQueries.HasActiveBatchActionEvents(ctx, r.GetConnOrTx(), taskID)
}

func (r *PgSchedRepo) DeleteBatchActionEvents(ctx common.ExtendedContext, taskID string) error {
	return r.eventQueries.DeleteBatchActionEvents(ctx, r.GetConnOrTx(), taskID)
}

func (r *PgSchedRepo) DeleteOldBatchActionRunEvents(ctx common.ExtendedContext, currentEventId string, taskID string, retention int32) error {
	return r.eventQueries.DeleteOldBatchActionRunEvents(ctx, r.GetConnOrTx(), events.DeleteOldBatchActionRunEventsParams{
		CurrentEventID:  currentEventId,
		TaskID:          taskID,
		Retention:       retention,
		PatronRequestID: events.DEFAULT_PATRON_REQUEST_ID,
	})
}

func (r *PgSchedRepo) GetScheduledTasks(ctx common.ExtendedContext, params GetScheduledTasksParams) ([]ScheduledTask, int64, error) {
	rows, err := r.queries.GetScheduledTasks(ctx, r.GetConnOrTx(), params)
	var tasks []ScheduledTask
	var fullCount int64
	if err == nil {
		if len(rows) > 0 {
			fullCount = rows[0].FullCount
			for _, r := range rows {
				tasks = append(tasks, r.ScheduledTask)
			}
		} else {
			params.Limit = 1
			params.Offset = 0
			rows, err = r.queries.GetScheduledTasks(ctx, r.GetConnOrTx(), params)
			if err == nil && len(rows) > 0 {
				fullCount = rows[0].FullCount
			}
		}
	}
	return tasks, fullCount, err
}

// QueueBatchActionRun reserves the owner and action and queues the run in the
// caller's transaction. The caller holds the scheduled-task lock.
func (r *PgSchedRepo) QueueBatchActionRun(ctx common.ExtendedContext, task ScheduledTask) error {
	if r.Tx == nil {
		return fmt.Errorf("queue batch action run requires a transaction")
	}
	data := task.ActionData.BatchActionData
	if data == nil {
		return fmt.Errorf("queue batch action run: missing batch action data")
	}
	switch schedoapi.BatchActionName(data.ActionName) {
	case schedoapi.EmailPullslips, schedoapi.RequestAging, schedoapi.Overdue:
		if err := r.eventQueries.LockBatchActionDispatch(ctx, r.Tx, data.ActionName); err != nil {
			return fmt.Errorf("lock batch action dispatch: %w", err)
		}
		conflict, err := r.eventQueries.GetConflictingBatchActionRun(ctx, r.Tx, events.GetConflictingBatchActionRunParams{
			ActionName: data.ActionName, Owner: task.Owner,
		})
		if err == nil {
			return &BatchActionRunActiveError{ID: conflict.ID, StartedAt: conflict.StartedAt.Time.UTC()}
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check batch action reservations: %w", err)
		}
	}
	id := uuid.NewString()
	eventRepo := &events.PgEventRepo{PgBaseRepo: repo.PgBaseRepo[events.EventRepo]{Pool: r.Pool, Tx: r.Tx}}
	_, err := eventRepo.SaveEvent(ctx, events.SaveEventParams{
		ID: id, IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID, PatronRequestID: events.DEFAULT_PATRON_REQUEST_ID,
		Timestamp: pgtype.Timestamp{Time: time.Now().UTC(), Valid: true}, EventType: events.EventTypeTask,
		EventName: events.EventNameInvokeBatchAction, EventStatus: events.EventStatusNew,
		EventData: task.ActionData, LastSignal: string(events.SignalTaskCreated),
	})
	if err != nil {
		return fmt.Errorf("queue batch action run: %w", err)
	}
	if err = r.eventQueries.AddBatchActionRun(ctx, r.Tx, events.AddBatchActionRunParams{
		ID: id, ScheduledTaskID: task.ID, Owner: task.Owner, ActionName: data.ActionName,
	}); err != nil {
		return fmt.Errorf("record batch action run: %w", err)
	}
	if err = r.eventQueries.AddBatchActionRunEvent(ctx, r.Tx, events.AddBatchActionRunEventParams{
		EventID: id, RunID: id, WorkerEventID: id,
	}); err != nil {
		return fmt.Errorf("link batch action run event: %w", err)
	}
	if err = eventRepo.Notify(ctx, id, events.SignalTaskCreated, events.SignalBatchWorkers); err != nil {
		return fmt.Errorf("notify batch action run: %w", err)
	}
	return nil
}
