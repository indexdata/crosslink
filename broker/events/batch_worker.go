package events

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var batchLeaseDuration, _ = utils.GetEnvAny("BATCH_WORKER_LEASE", 5*time.Minute, func(value string) (time.Duration, error) {
	duration, err := time.ParseDuration(value)
	if err != nil || duration < time.Second || duration > batchWorkerCap {
		return 0, fmt.Errorf("BATCH_WORKER_LEASE must be between 1s and %s", batchWorkerCap)
	}
	return duration, nil
})

const (
	batchWorkerCap     = 30 * time.Minute
	batchWorkerCount   = 8
	batchPollInterval  = 15 * time.Second
	batchRecoveryLimit = 100
)

type batchClaimKey struct{}

func consumeBatchClaim(ctx context.Context) bool {
	claimed, _ := ctx.Value(batchClaimKey{}).(*atomic.Bool)
	return claimed != nil && claimed.CompareAndSwap(false, true)
}

type batchWorkers struct {
	wake   chan struct{}
	stop   context.CancelFunc
	cancel context.CancelFunc
	done   chan struct{}
}

// WithWorkerContext propagates the lease to worker-originated event writes.
func WithWorkerContext(bus EventBus, ctx common.ExtendedContext) EventBus {
	if postgres, ok := bus.(*PostgresEventBus); ok && repo.BatchWorker(ctx) != "" {
		scoped := *postgres
		scoped.ctx = ctx
		return &scoped
	}
	return bus
}

func (p *PostgresEventBus) startBatchWorkers(db *PgEventRepo) {
	var names []string
	for _, name := range []EventName{EventNameInvokeBatchAction, EventNameInvokeBackgroundAction} {
		if len(p.getHandlers(SignalTaskCreated, HandlerRoleConsumer, name)) > 0 {
			names = append(names, string(name))
		}
	}
	if len(names) == 0 {
		return
	}
	claims, stop := context.WithCancel(p.ctx)
	work, cancel := context.WithCancel(p.ctx)
	workers := &batchWorkers{wake: make(chan struct{}, 1), stop: stop, cancel: cancel, done: make(chan struct{})}
	p.batch = workers

	var wait sync.WaitGroup
	for range batchWorkerCount {
		wait.Go(func() {
			for claims.Err() == nil {
				event, deadline, err := db.claimBatchWork(p.batchContext(claims, ""), batchLeaseDuration, batchWorkerCap, names)
				if err == nil {
					p.wakeBatchWorkers()
					p.runBatchWorker(work, db, event, deadline)
					continue
				}
				if !errors.Is(err, pgx.ErrNoRows) && claims.Err() == nil {
					p.ctx.Logger().Error("claim batch worker event", "error", err)
				}
				select {
				case <-claims.Done():
					return
				case <-workers.wake:
				}
			}
		})
	}
	wait.Go(func() {
		ticker := time.NewTicker(batchPollInterval)
		defer ticker.Stop()
		for claims.Err() == nil {
			p.wakeBatchWorkers()
			ctx, cancel := context.WithTimeout(claims, batchPollInterval)
			if err := db.recoverBatchWork(p.batchContext(ctx, "")); err != nil && claims.Err() == nil {
				p.ctx.Logger().Error("recover batch worker events", "error", err)
			}
			cancel()
			select {
			case <-claims.Done():
				return
			case <-ticker.C:
			}
		}
	})
	go func() {
		wait.Wait()
		cancel()
		close(workers.done)
	}()
}

// DrainBatchWorkers stops new claims and waits for current handlers or the deadline.
func (p *PostgresEventBus) DrainBatchWorkers(ctx context.Context) error {
	if p.batch == nil {
		return nil
	}
	p.batch.stop()
	select {
	case <-p.batch.done:
		return nil
	case <-ctx.Done():
		p.batch.cancel()
		return ctx.Err()
	}
}

func (p *PostgresEventBus) wakeBatchWorkers() {
	if p.batch != nil {
		select {
		case p.batch.wake <- struct{}{}:
		default:
		}
	}
}

func (p *PostgresEventBus) batchContext(ctx context.Context, eventID string) common.ExtendedContext {
	args := p.ctx.LoggerArgs()
	args.EventId = eventID
	return common.CreateExtCtxWithArgs(ctx, &args)
}

func (p *PostgresEventBus) runBatchWorker(work context.Context, db *PgEventRepo, event Event, initial time.Time) {
	ctx, cancel := context.WithCancel(work)
	defer cancel()
	var deadline atomic.Value
	deadline.Store(initial)
	leaseDeadline := func() time.Time { return deadline.Load().(time.Time) }
	claimCtx := context.WithValue(ctx, batchClaimKey{}, new(atomic.Bool))
	workerCtx := repo.WithBatchWorker(p.batchContext(claimCtx, event.ID), event.ID, leaseDeadline)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		defer cancel()
		ticker := time.NewTicker(batchLeaseDuration / 3)
		defer ticker.Stop()
		for {
			remaining := time.Until(leaseDeadline())
			timer := time.NewTimer(remaining)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				return
			case <-ticker.C:
				timer.Stop()
			}

			start := time.Now()
			renewCtx, stop := context.WithDeadline(ctx, leaseDeadline())
			renewed, err := db.queries.RenewBatchWorkerLease(renewCtx, db.Pool, RenewBatchWorkerLeaseParams{
				EventID: event.ID, LeaseDuration: batchInterval(batchLeaseDuration),
			})
			stop()
			if err != nil {
				if ctx.Err() == nil && !errors.Is(err, pgx.ErrNoRows) {
					workerCtx.Logger().Warn("renew batch worker lease", "error", err)
				}
				return
			}
			deadline.Store(start.Add(renewed.LeaseExpiresAt.Time.Sub(renewed.DatabaseNow.Time)))
		}
	}()

	handlers := p.getHandlers(SignalTaskCreated, HandlerRoleConsumer, event.EventName)
	triggerHandlers(workerCtx, event, handlers, SignalTaskCreated)
	cancel()
	<-renewDone
}

func (p *PostgresEventBus) delegatedBatchContext(ctx common.ExtendedContext, eventID string) (common.ExtendedContext, context.CancelFunc, error) {
	db, ok := p.repo.(*PgEventRepo)
	if !ok {
		return ctx, nil, repo.ErrBatchLeaseLost
	}
	start := time.Now()
	lease, err := db.queries.GetActiveBatchWorkerLease(ctx, db.Pool, eventID)
	if err != nil {
		return ctx, nil, err
	}
	deadline := start.Add(lease.LeaseExpiresAt.Time.Sub(lease.DatabaseNow.Time))
	bounded, cancel := context.WithDeadline(ctx, deadline)
	return repo.WithBatchWorker(p.batchContext(bounded, ctx.LoggerArgs().EventId), eventID, func() time.Time { return deadline }), cancel, nil
}

func batchInterval(duration time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: duration.Microseconds(), Valid: true}
}

// A durable claim consumes the creation signal and begins the event atomically.
func (r *PgEventRepo) claimBatchWork(ctx common.ExtendedContext, lease, cap time.Duration, names []string) (Event, time.Time, error) {
	var event Event
	var deadline time.Time
	err := r.WithTxFunc(ctx, func(bound EventRepo) error {
		tx := bound.(*PgEventRepo)
		id, err := tx.queries.NextBatchWorkerEvent(ctx, tx.Tx, names)
		if err != nil {
			return err
		}
		start := time.Now()
		window, err := tx.queries.StartBatchWorkerLease(ctx, tx.Tx, StartBatchWorkerLeaseParams{
			EventID: id, LeaseDuration: batchInterval(lease), LeaseCap: batchInterval(cap),
		})
		if err != nil {
			return err
		}
		deadline = start.Add(window.LeaseExpiresAt.Time.Sub(window.DatabaseNow.Time))
		event, err = tx.UpdateEventLifecycle(ctx, UpdateEventLifecycleParams{
			ID: id, EventStatus: EventStatusProcessing, LastSignal: string(SignalTaskBegin),
		})
		if err != nil {
			return err
		}
		return tx.Notify(ctx, id, SignalTaskBegin, SignalConsumers)
	})
	return event, deadline, err
}

func (r *PgEventRepo) recoverBatchWork(ctx common.ExtendedContext) error {
	interrupted, err := r.queries.GetInterruptedBatchWorkers(ctx, r.Pool, batchRecoveryLimit)
	if err != nil {
		return err
	}
	var failures []error
	for _, worker := range interrupted {
		if err := r.expireBatchWorker(ctx, worker.WorkerID, worker.RunID); err != nil {
			failures = append(failures, fmt.Errorf("expire worker %s: %w", worker.WorkerID, err))
		}
	}
	// Cascading request deletion can remove a run's final child.
	idle, err := r.queries.ListIdleBatchRuns(ctx, r.Pool, batchRecoveryLimit)
	if err != nil {
		return errors.Join(append(failures, err)...)
	}
	for _, id := range idle {
		err := r.WithTxFunc(ctx, func(bound EventRepo) error {
			tx := bound.(*PgEventRepo)
			row, err := tx.queries.LockBatchRun(ctx, tx.Tx, id)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			return tx.finishBatchRunIfIdle(ctx, row.BatchActionRun)
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("finish idle run %s: %w", id, err))
		}
	}
	return errors.Join(failures...)
}

func (r *PgEventRepo) expireBatchWorker(ctx common.ExtendedContext, workerID, runID string) error {
	return r.WithTxFunc(ctx, func(bound EventRepo) error {
		tx := bound.(*PgEventRepo)
		row, err := tx.queries.LockBatchRun(ctx, tx.Tx, runID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // Finished or being updated by another replica.
		}
		if err != nil {
			return err
		}
		if _, err = tx.queries.LockExpiredBatchWorker(ctx, tx.Tx, workerID); errors.Is(err, pgx.ErrNoRows) {
			// A present worker renewed, finished, or is still committing a
			// fenced write. A deleted worker can never reclaim its event ID.
			if _, err = tx.queries.GetBatchEventWorker(ctx, tx.Tx, workerID); !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := tx.finishAbandonedWorkerEvents(ctx, workerID); err != nil {
			return err
		}
		if err := tx.queries.ClearIdleBatchWorkerLease(ctx, tx.Tx, workerID); err != nil {
			return err
		}
		return tx.finishBatchRunIfIdle(ctx, row.BatchActionRun)
	})
}

func (r *PgEventRepo) finishAbandonedWorkerEvents(ctx common.ExtendedContext, id string) error {
	ids, err := r.queries.ListUnfinishedBatchWorkerEvents(ctx, r.Tx, id)
	if err != nil {
		return err
	}
	for _, eventID := range ids {
		event, err := r.GetEventForUpdate(ctx, eventID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // The request and its event were deleted.
		}
		if err != nil {
			return err
		}
		status, result := NewErrorResult("batch worker interrupted", "The worker did not complete this task.")
		if _, err := completeEvent(ctx, r, event, result, status, SignalConsumers); err != nil {
			return err
		}
		if err := r.queries.FinishBatchRunEvent(ctx, r.Tx, eventID); err != nil {
			return err
		}
	}
	return nil
}

func (r *PgEventRepo) lockBatchRunForEvent(ctx common.ExtendedContext, id string) (BatchActionRun, error) {
	row, err := r.queries.LockBatchRunForEvent(ctx, r.Tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return BatchActionRun{}, nil
	}
	return row.BatchActionRun, err
}

// finishBatchRunIfIdle requires the run lock, which also serializes child creation.
func (r *PgEventRepo) finishBatchRunIfIdle(ctx common.ExtendedContext, run BatchActionRun) error {
	idle, err := r.queries.IsBatchRunIdle(ctx, r.Tx, run.ID)
	if err != nil || !idle {
		return err
	}
	return r.queries.FinishBatchRun(ctx, r.Tx, run.ID)
}

// Only batch actions, their inline children, and delegated deliveries can
// belong to a run. A caller that supplies only an ID requires a DB check.
func batchOwnershipRequired(ctx common.ExtendedContext, event Event) bool {
	switch event.EventName {
	case "", EventNameInvokeBatchAction, EventNameInvokeBackgroundAction, EventNameInvokeAction, EventNamePatronRequestMessage:
		return true
	default:
		return repo.BatchWorker(ctx) != "" || event.EventData.BatchWorkerID != ""
	}
}

func (r *PgEventRepo) checkBatchEventOwnership(ctx common.ExtendedContext, eventID string) error {
	owner, err := r.queries.GetBatchEventWorker(ctx, r.Tx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return repo.LockBatchWorker(ctx, r.Tx)
	}
	if err != nil {
		return err
	}
	if owner != repo.BatchWorker(ctx) {
		return repo.ErrBatchLeaseLost
	}
	return repo.LockBatchWorker(ctx, r.Tx)
}
