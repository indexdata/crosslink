package events

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	batchRecoveryInterval = 15 * time.Second
	batchRecoveryTimeout  = 5 * time.Second
	batchRecoveryPageSize = 100
	batchWorkerLimit      = 8
)

type batchWorkers struct {
	slots          chan struct{}
	wake           chan struct{}
	waitingForSlot atomic.Bool
	rescan         atomic.Bool
}

func (w *batchWorkers) wakeRecoveryWithCapacity() {
	// Set the flag before checking capacity so a concurrent release can wake us.
	w.waitingForSlot.Store(true)
	if len(w.slots) < cap(w.slots) && w.waitingForSlot.Swap(false) {
		w.wakeRecovery()
	}
}

func (w *batchWorkers) wakeRecovery() {
	if w == nil {
		return
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// holdBatchWorkerSession holds a dedicated PostgreSQL session lock until the
// handler returns. Cancellation alone never releases the lock of live work.
func (p *PostgresEventBus) holdBatchWorkerSession() (string, func(), error) {
	if p.batchWorkers == nil {
		return "", nil, pgx.ErrNoRows
	}
	workers := p.batchWorkers
	select {
	case workers.slots <- struct{}{}:
	default:
		workers.wakeRecoveryWithCapacity()
		return "", nil, pgx.ErrNoRows
	}
	releaseSlot := func() {
		<-workers.slots
		if workers.waitingForSlot.Swap(false) {
			workers.wakeRecovery()
		}
	}
	ctx, cancel := context.WithTimeout(p.ctx, batchRecoveryTimeout)
	defer cancel()
	conn, err := pgx.Connect(ctx, p.ConnectionString)
	if err != nil {
		releaseSlot()
		return "", nil, fmt.Errorf("connect batch worker session: %w", err)
	}
	release := func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), batchRecoveryTimeout)
		defer cancel()
		_ = conn.Close(closeCtx)
		releaseSlot()
	}
	lockID := uuid.NewString()
	_, err = conn.Exec(ctx, "SELECT pg_advisory_lock(hashtextextended('broker:batch-worker:' || $1::text, 0))", lockID)
	if err != nil {
		release()
		return "", nil, fmt.Errorf("lock batch worker session: %w", err)
	}
	return lockID, release, nil
}

func (p *PostgresEventBus) claimNotifiedEvent(eventRepo *PgEventRepo, id string, signal Signal, hasConsumer bool) (Event, repo.BatchAttempt, func(), error) {
	var lockID string
	var release func()
	if signal == SignalTaskCreated && hasConsumer && p.batchWorkers != nil {
		// This read avoids opening sessions for stale notifications. The claim
		// below still checks ownership and signal eligibility transactionally.
		eligible, err := eventRepo.queries.GetBatchTaskClaimEligibility(p.ctx, eventRepo.GetConnOrTx(), GetBatchTaskClaimEligibilityParams{
			EventID: id, LastSignal: string(signal),
		})
		switch {
		case err == nil:
			if !eligible.Bool {
				return Event{}, repo.BatchAttempt{}, nil, pgx.ErrNoRows
			}
			lockID, release, err = p.holdBatchWorkerSession()
			if err != nil {
				return Event{}, repo.BatchAttempt{}, nil, err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return Event{}, repo.BatchAttempt{}, nil, err
		}
	}
	event, attempt, err := eventRepo.claimEventAttemptForSignal(p.ctx, id, signal, hasConsumer, lockID)
	return event, attempt, release, err
}

func (p *PostgresEventBus) runBatchRecovery(eventRepo *PgEventRepo, consumerNames []string) {
	ticker := time.NewTicker(batchRecoveryInterval)
	defer ticker.Stop()
	var after string
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
		case <-p.batchWorkers.wake:
		}
		if p.batchWorkers.rescan.Swap(false) {
			after = ""
		}
		ctx, cancel := context.WithTimeout(p.ctx, batchRecoveryTimeout)
		args := p.ctx.LoggerArgs()
		previous := after
		after = p.recoverBatchPage(common.CreateExtCtxWithArgs(ctx, &args), eventRepo, consumerNames, after)
		cancel()
		if after != "" && after != previous {
			p.batchWorkers.wakeRecoveryWithCapacity()
		}
	}
}

func (p *PostgresEventBus) recoverBatchPage(ctx common.ExtendedContext, eventRepo *PgEventRepo, consumerNames []string, after string) string {
	ids, err := eventRepo.queries.ListBatchRecoveryCandidates(ctx, eventRepo.GetConnOrTx(), ListBatchRecoveryCandidatesParams{
		AfterID: after, ConsumerNames: consumerNames, PageSize: batchRecoveryPageSize,
	})
	if err != nil {
		if ctx.Err() == nil {
			ctx.Logger().Error("list coordinated batch work for recovery", "error", err)
		}
		return after
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return after
		}
		if err := eventRepo.recoverBatchEvent(ctx, id); err != nil && ctx.Err() == nil {
			ctx.Logger().Error("recover coordinated batch event", "eventId", id, "error", err)
		}
		after = id
	}
	if len(ids) < batchRecoveryPageSize {
		return ""
	}
	return after
}

func (r *PgEventRepo) recoverBatchEvent(ctx common.ExtendedContext, id string) error {
	return r.WithTxFunc(ctx, func(eventRepo EventRepo) error {
		tx := eventRepo.(*PgEventRepo)
		_, err := tx.queries.LockBatchRecoveryCandidate(ctx, tx.Tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// Read ownership after locking the event row. Claim, checkpoints, and
		// fenced writes also serialize on that row.
		candidate, err := tx.queries.GetBatchRecoveryCandidate(ctx, tx.Tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if candidate.RecoveryBlockedAt.Valid ||
			(candidate.Event.EventStatus != EventStatusNew && candidate.Event.EventStatus != EventStatusProcessing) {
			return nil
		}
		if candidate.AttemptID.Valid {
			if !candidate.AttemptLockID.Valid {
				return tx.blockBatchRecovery(ctx, id, "previous attempt has no verifiable worker session")
			}
			abandoned, err := tx.queries.TryBatchRecoveryLock(ctx, tx.Tx, candidate.AttemptLockID.String)
			if err != nil || !abandoned {
				return err
			}
			// Keep the advisory lock through commit. The previous token can no
			// longer pass any write fence once this transaction commits.
			if err := tx.queries.ClearBatchActionAttempt(ctx, tx.Tx, id); err != nil {
				return err
			}
		} else if candidate.Event.EventStatus == EventStatusProcessing {
			return tx.blockBatchRecovery(ctx, id, "processing event has no verifiable attempt")
		}

		reason, err := tx.batchReplayBlockReason(ctx, candidate)
		if err != nil {
			return err
		}
		if reason != "" {
			return tx.blockBatchRecovery(ctx, id, reason)
		}
		if candidate.Event.EventName == EventNameInvokeAction {
			// A live parent will start unclaimed inline children itself.
			return nil
		}
		if candidate.Event.EventStatus != EventStatusNew || candidate.Event.LastSignal != string(SignalTaskCreated) {
			if _, err := tx.UpdateEventLifecycle(ctx, UpdateEventLifecycleParams{
				ID: id, EventStatus: EventStatusNew, LastSignal: string(SignalTaskCreated),
			}); err != nil {
				return err
			}
		}
		return tx.Notify(ctx, id, SignalTaskCreated, SignalConsumers)
	})
}

func (r *PgEventRepo) batchReplayBlockReason(ctx common.ExtendedContext, candidate GetBatchRecoveryCandidateRow) (string, error) {
	event := candidate.Event
	if candidate.WorkStartedAt.Valid {
		return "work may already have changed state or caused external effects", nil
	}
	if event.EventName == EventNameInvokeAction {
		if !candidate.AttemptID.Valid && event.ParentID.Valid {
			parent, err := r.queries.GetBatchRecoveryCandidate(ctx, r.Tx, event.ParentID.String)
			if err == nil && parent.Event.EventStatus == EventStatusProcessing && !parent.RecoveryBlockedAt.Valid {
				return "", nil
			}
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return "", err
			}
		}
		return "inline action requires reconciliation with its parent", nil
	}
	if event.EventName != EventNameInvokeBatchAction {
		return "", nil
	}
	switch candidate.ActionName {
	case "request-aging", "overdue":
		return "", nil
	case "email-pullslips":
		delivery, err := r.queries.GetBatchEmailDelivery(ctx, r.Tx, candidate.RunID)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		if !delivery.BatchActionEmailDelivery.AcceptedAt.Valid {
			return "SMTP acceptance is unconfirmed", nil
		}
		return "", nil
	default:
		if event.EventStatus != EventStatusNew {
			return "started batch action has no safe replay checkpoint", nil
		}
		return "", nil
	}
}

func (r *PgEventRepo) blockBatchRecovery(ctx common.ExtendedContext, eventID, reason string) error {
	err := r.queries.BlockBatchActionRecovery(ctx, r.Tx, BlockBatchActionRecoveryParams{
		EventID: eventID, Reason: pgtype.Text{String: reason, Valid: true},
	})
	if err == nil {
		ctx.Logger().Warn("coordinated batch work requires investigation", "eventId", eventID, "reason", reason)
	}
	return err
}
