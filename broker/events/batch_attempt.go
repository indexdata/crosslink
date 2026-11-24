package events

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// WithWorkerContext propagates a coordinated attempt to task and notice creation.
func WithWorkerContext(bus EventBus, ctx common.ExtendedContext) EventBus {
	if postgres, ok := bus.(*PostgresEventBus); ok && repo.BatchAttemptFrom(ctx).ID != "" {
		scoped := *postgres
		scoped.ctx = ctx
		return &scoped
	}
	return bus
}

// ClaimEventAttemptForSignal claims the signal and persists a worker attempt
// for coordinated task creation in the same transaction.
func (r *PgEventRepo) ClaimEventAttemptForSignal(ctx common.ExtendedContext, id string, signal Signal) (Event, repo.BatchAttempt, error) {
	return r.claimEventAttemptForSignal(ctx, id, signal, true)
}

func (r *PgEventRepo) claimEventAttemptForSignal(ctx common.ExtendedContext, id string, signal Signal, hasConsumer bool) (Event, repo.BatchAttempt, error) {
	if signal != SignalTaskCreated {
		event, err := r.claimEventSignal(ctx, id, signal)
		return event, repo.BatchAttempt{}, err
	}
	var event Event
	var attempt repo.BatchAttempt
	claim := func(eventRepo EventRepo) error {
		tx := eventRepo.(*PgEventRepo)
		var err error
		event, err = tx.claimEventSignal(ctx, id, signal)
		if err != nil {
			return err
		}
		current, err := tx.queries.GetBatchActionAttempt(ctx, tx.Tx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read batch action attempt: %w", err)
		}
		if !hasConsumer || event.EventStatus != EventStatusNew || current.Valid {
			return pgx.ErrNoRows
		}
		attempt, err = tx.assignBatchActionAttempt(ctx, id)
		return err
	}
	err := r.WithTxFunc(ctx, claim)
	if err != nil {
		return Event{}, repo.BatchAttempt{}, err
	}
	return event, attempt, nil
}

// assignBatchActionAttempt requires the event row lock.
func (r *PgEventRepo) assignBatchActionAttempt(ctx common.ExtendedContext, eventID string) (repo.BatchAttempt, error) {
	attempt := repo.BatchAttempt{EventID: eventID, ID: uuid.NewString()}
	count, err := r.queries.AssignBatchActionAttempt(ctx, r.Tx, AssignBatchActionAttemptParams{
		EventID: eventID, AttemptID: pgtype.Text{String: attempt.ID, Valid: true},
	})
	if err != nil {
		return repo.BatchAttempt{}, fmt.Errorf("assign batch action attempt: %w", err)
	}
	if count != 1 {
		return repo.BatchAttempt{}, repo.ErrBatchAttemptLost
	}
	return attempt, nil
}

func (r *PgEventRepo) beginBatchActionAttempt(ctx common.ExtendedContext, event Event) (common.ExtendedContext, error) {
	current, err := r.queries.GetBatchActionAttempt(ctx, r.Tx, event.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ctx, nil
	}
	if err != nil {
		return ctx, fmt.Errorf("read batch action attempt: %w", err)
	}
	attempt := repo.BatchAttemptFrom(ctx)
	if current.Valid && current.String == attempt.ID && event.ID == attempt.EventID {
		return ctx, nil
	}

	// Inline auto actions claim their own attempt while moving to PROCESSING.
	// Their parent must still own its attempt in this same transaction.
	if current.Valid || attempt.ID == "" || event.EventName != EventNameInvokeAction ||
		!event.ParentID.Valid || event.ParentID.String != attempt.EventID {
		return ctx, repo.ErrBatchAttemptLost
	}
	attempt, err = r.assignBatchActionAttempt(ctx, event.ID)
	if err != nil {
		return ctx, err
	}
	return repo.WithBatchAttempt(ctx, attempt), nil
}

func (r *PgEventRepo) requireBatchActionAttempt(ctx common.ExtendedContext, eventID string) error {
	_, err := r.queries.GetBatchActionAttempt(ctx, r.Tx, eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read batch action attempt: %w", err)
	}
	attempt := repo.BatchAttemptFrom(ctx)
	if attempt.ID == "" || attempt.EventID != eventID {
		return repo.ErrBatchAttemptLost
	}
	return repo.LockBatchAttempt(ctx, r.Tx)
}
