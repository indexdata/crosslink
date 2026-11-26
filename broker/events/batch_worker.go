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

func (p *PostgresEventBus) startBatchWorkers(db *PgEventRepo) {
	p.handlersMu.RLock()
	consumers := p.handlers[SignalTaskCreated][HandlerRoleConsumer]
	names := make([]string, 0, len(consumers))
	for name := range consumers {
		names = append(names, string(name))
	}
	p.handlersMu.RUnlock()
	if len(names) == 0 {
		return
	}
	leaseDuration, err := utils.GetEnvAny("BATCH_WORKER_LEASE", 5*time.Minute, func(value string) (time.Duration, error) {
		duration, err := time.ParseDuration(value)
		if err != nil || duration < time.Second || duration > batchWorkerCap {
			return 0, fmt.Errorf("BATCH_WORKER_LEASE must be between 1s and %s", batchWorkerCap)
		}
		return duration, nil
	})
	if err != nil {
		p.ctx.Logger().Warn("using default batch worker lease", "error", err, "lease", leaseDuration)
	}
	claims, stop := context.WithCancel(p.ctx)
	work, cancel := context.WithCancel(p.ctx)
	workers := &batchWorkers{wake: make(chan struct{}, 1), stop: stop, cancel: cancel, done: make(chan struct{})}
	p.batch = workers

	var wait sync.WaitGroup
	for range batchWorkerCount {
		wait.Go(func() {
			for claims.Err() == nil {
				event, deadline, err := db.claimBatchWork(p.batchContext(claims, ""), leaseDuration, batchWorkerCap, names)
				if err == nil {
					p.wakeBatchWorkers()
					p.runBatchWorker(work, event, deadline, leaseDuration, func(ctx context.Context) (time.Time, error) {
						return db.renewBatchWorkerLease(ctx, event.ID, leaseDuration)
					})
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

func (p *PostgresEventBus) runBatchWorker(work context.Context, event Event, initial time.Time, leaseDuration time.Duration, renew func(context.Context) (time.Time, error)) {
	if work.Err() != nil || time.Until(initial) <= 0 {
		return
	}
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
		renewInterval := leaseDuration / 3
		retryInterval := min(time.Second, leaseDuration/10)
		delay := renewInterval
		for {
			expires := leaseDeadline()
			timer := time.NewTimer(min(delay, time.Until(expires)))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			if ctx.Err() != nil || time.Until(expires) <= 0 {
				return
			}
			renewCtx, stop := context.WithDeadline(ctx, expires)
			renewed, err := renew(renewCtx)
			stop()
			if ctx.Err() != nil || time.Until(expires) <= 0 || errors.Is(err, pgx.ErrNoRows) {
				return
			}
			if err != nil {
				workerCtx.Logger().Warn("retrying batch worker lease renewal", "error", err)
				delay = retryInterval
				continue
			}
			deadline.Store(renewed)
			delay = renewInterval
		}
	}()

	handlers := p.getHandlers(SignalTaskCreated, HandlerRoleConsumer, event.EventName)
	triggerHandlers(workerCtx, event, handlers, SignalTaskCreated)
	if db, ok := p.repo.(*PgEventRepo); ok {
		db.waitForBatchDependents(workerCtx, event.ID, min(time.Second, leaseDuration/3))
	}
	cancel()
	<-renewDone
}

// A completed worker can still own notified descendants. An interrupted
// worker leaves its lease to expire even if descendants remain.
func (r *PgEventRepo) waitForBatchDependents(ctx common.ExtendedContext, id string, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		pending, err := r.queries.HasFinishedBatchWorkerDependents(ctx, r.Pool, id)
		if err == nil && !pending {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *PgEventRepo) renewBatchWorkerLease(ctx context.Context, eventID string, lease time.Duration) (time.Time, error) {
	start := time.Now()
	renewed, err := r.queries.RenewBatchWorkerLease(ctx, r.Pool, RenewBatchWorkerLeaseParams{
		EventID: eventID, LeaseDuration: batchInterval(lease),
	})
	if err != nil {
		return time.Time{}, err
	}
	return start.Add(renewed.LeaseExpiresAt.Time.Sub(renewed.DatabaseNow.Time)), nil
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
	initial := start.Add(lease.LeaseExpiresAt.Time.Sub(lease.DatabaseNow.Time))
	if time.Until(initial) <= 0 {
		return ctx, nil, pgx.ErrNoRows
	}
	watchCtx, cancel := context.WithCancel(ctx)
	var deadline atomic.Value
	deadline.Store(initial)
	lastConfirmedDeadline := func() time.Time { return deadline.Load().(time.Time) }
	delegated := repo.WithBatchWorker(p.batchContext(watchCtx, ctx.LoggerArgs().EventId), eventID, lastConfirmedDeadline)
	go func() {
		defer cancel()
		// A delegate may be on another replica. Only its owner renews the lease.
		confirmedExpiry := lease.LeaseExpiresAt.Time
		refreshDelay := func(remaining time.Duration) time.Duration {
			return max(10*time.Millisecond, min(time.Second, remaining/3))
		}
		delay := refreshDelay(time.Until(initial))
		for {
			expires := lastConfirmedDeadline()
			timer := time.NewTimer(min(delay, time.Until(expires)))
			select {
			case <-watchCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if watchCtx.Err() != nil || time.Until(expires) <= 0 {
				return
			}
			start := time.Now()
			refreshCtx, stop := context.WithDeadline(watchCtx, expires)
			current, err := db.queries.GetActiveBatchWorkerLease(refreshCtx, db.Pool, eventID)
			stop()
			if watchCtx.Err() != nil || time.Until(expires) <= 0 || errors.Is(err, pgx.ErrNoRows) {
				return
			}
			if err != nil {
				delegated.Logger().Debug("refresh delegated batch lease", "error", err)
				continue
			}
			if !current.LeaseExpiresAt.Time.Equal(confirmedExpiry) {
				confirmedExpiry = current.LeaseExpiresAt.Time
				updated := start.Add(confirmedExpiry.Sub(current.DatabaseNow.Time))
				// An unchanged database expiry would only move the local
				// estimate as connection latency changes.
				deadline.Store(updated)
				delay = refreshDelay(time.Until(updated))
			}
		}
	}()
	return delegated, cancel, nil
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
		row, err := tx.queries.BeginBatchWorkerEvent(ctx, tx.Tx, id)
		if err != nil {
			return err
		}
		event = row.Event
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
			// A present worker renewed, finished, or still holds the lease lock.
			// A deleted worker can never reclaim its event ID.
			if _, err = tx.queries.GetBatchRunEvent(ctx, tx.Tx, workerID); !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := tx.queries.FinishAbandonedWorkerEvents(ctx, tx.Tx, workerID); err != nil {
			return err
		}
		if err := tx.queries.ClearIdleBatchWorkerLease(ctx, tx.Tx, workerID); err != nil {
			return err
		}
		return tx.finishBatchRunIfIdle(ctx, row.BatchActionRun)
	})
}

type lockedBatchEvent struct {
	BatchActionRun
	BatchActionRunEvent
}

func (r *PgEventRepo) lockBatchRunForEvent(ctx common.ExtendedContext, id string) (lockedBatchEvent, error) {
	row, err := r.queries.LockBatchRunForEvent(ctx, r.Tx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockedBatchEvent{}, nil
	}
	if err != nil {
		return lockedBatchEvent{}, err
	}
	// Read membership after taking the run lock. A finishing transaction
	// may have changed it while this transaction waited for that lock.
	member, err := r.queries.GetBatchRunEvent(ctx, r.Tx, id)
	return lockedBatchEvent{row.BatchActionRun, member.BatchActionRunEvent}, err
}

func batchParent(ctx common.ExtendedContext, eventRepo EventRepo, parentID *string) (lockedBatchEvent, error) {
	tx, ok := eventRepo.(*PgEventRepo)
	if !ok || parentID == nil {
		return lockedBatchEvent{}, nil
	}
	parent, err := tx.lockBatchRunForEvent(ctx, *parentID)
	if err != nil || parent.ID == "" {
		return parent, err
	}
	if parent.CompletedAt.Valid || parent.Finished {
		return parent, repo.ErrBatchLeaseLost
	}
	return parent, tx.lockBatchWorker(ctx, parent.WorkerEventID)
}

func (r *PgEventRepo) addBatchEvent(ctx common.ExtendedContext, id, runID, workerID string) error {
	if err := r.queries.AddBatchActionRunEvent(ctx, r.Tx, AddBatchActionRunEventParams{
		EventID: id, RunID: runID, WorkerEventID: workerID,
	}); err != nil {
		return fmt.Errorf("link batch child event: %w", err)
	}
	return nil
}

func lockOwnedBatchEvent(ctx common.ExtendedContext, eventRepo EventRepo, id string) (lockedBatchEvent, error) {
	tx, ok := eventRepo.(*PgEventRepo)
	if !ok {
		return lockedBatchEvent{}, nil
	}
	member, err := tx.lockBatchRunForEvent(ctx, id)
	if err != nil || member.ID == "" {
		return member, err
	}
	if member.Finished || member.WorkerEventID != repo.BatchWorker(ctx) {
		return member, repo.ErrBatchLeaseLost
	}
	return member, tx.lockBatchWorker(ctx, member.WorkerEventID)
}

func (p *PostgresEventBus) finishBatchEvent(ctx common.ExtendedContext, id string) error {
	return p.repo.WithTxFunc(ctx, func(eventRepo EventRepo) error {
		member, err := lockOwnedBatchEvent(ctx, eventRepo, id)
		if err != nil || member.ID == "" {
			return err
		}
		// Completion callbacks are invoked directly for batch tasks, so
		// clear their pending signal before releasing the reservation.
		if _, err := eventRepo.ClaimEventForSignal(ctx, id, SignalTaskComplete); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return eventRepo.(*PgEventRepo).finishBatchEvent(ctx, id, member)
	})
}

func (r *PgEventRepo) finishBatchEvent(ctx common.ExtendedContext, id string, member lockedBatchEvent) error {
	if err := r.queries.FinishBatchRunEvent(ctx, r.Tx, id); err != nil {
		return err
	}
	if err := r.queries.ClearIdleBatchWorkerLease(ctx, r.Tx, member.WorkerEventID); err != nil {
		return err
	}
	return r.finishBatchRunIfIdle(ctx, member.BatchActionRun)
}

// finishBatchRunIfIdle requires the run lock, which also serializes child creation.
func (r *PgEventRepo) finishBatchRunIfIdle(ctx common.ExtendedContext, run BatchActionRun) error {
	idle, err := r.queries.IsBatchRunIdle(ctx, r.Tx, run.ID)
	if err != nil || !idle {
		return err
	}
	return r.queries.FinishBatchRun(ctx, r.Tx, run.ID)
}

// lockBatchWorker holds the lease through the event bus transaction's commit.
func (r *PgEventRepo) lockBatchWorker(ctx context.Context, id string) error {
	var current bool
	if err := r.Tx.QueryRow(ctx, "SELECT lock_batch_worker($1)", id).Scan(&current); err != nil {
		return fmt.Errorf("check batch worker lease: %w", err)
	}
	if !current {
		return repo.ErrBatchLeaseLost
	}
	return nil
}
