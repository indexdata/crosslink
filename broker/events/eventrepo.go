package events

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
	"github.com/jackc/pgx/v5"
)

type EventRepo interface {
	repo.Transactional[EventRepo]
	SaveEvent(ctx common.ExtendedContext, params SaveEventParams) (Event, error)
	LinkBatchActionChild(ctx common.ExtendedContext, parentID, eventID string) error
	FinishBatchActionRunIfIdle(ctx common.ExtendedContext, eventID string) error
	UpdateEventLifecycle(ctx common.ExtendedContext, params UpdateEventLifecycleParams) (Event, error)
	GetEvent(ctx common.ExtendedContext, id string) (Event, error)
	GetEventForUpdate(ctx common.ExtendedContext, id string) (Event, error)
	GetOlderIncompleteEvent(ctx common.ExtendedContext, event Event) (Event, error)
	// ClaimEventForSignal atomically clears and claims one pending lifecycle signal.
	ClaimEventForSignal(ctx common.ExtendedContext, id string, signal Signal) (Event, error)
	Notify(ctx common.ExtendedContext, eventId string, signal Signal, target SignalTarget) error
	GetIllTransactionEvents(ctx common.ExtendedContext, id string) ([]Event, int64, error)
	GetBatchActionEvents(ctx common.ExtendedContext, taskID string) ([]Event, error)
	GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (Event, error)
	GetPatronRequestEvents(ctx common.ExtendedContext, id string) ([]Event, error)
}

type PgEventRepo struct {
	repo.PgBaseRepo[EventRepo]
	queries Queries
}

// delegate transaction handling to Base
func (r *PgEventRepo) WithTxFunc(ctx common.ExtendedContext, fn func(EventRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// DerivedRepo
func (r *PgEventRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[EventRepo]) EventRepo {
	eventRepo := new(PgEventRepo)
	eventRepo.PgBaseRepo = *base
	return eventRepo
}

func (r *PgEventRepo) SaveEvent(ctx common.ExtendedContext, params SaveEventParams) (Event, error) {
	row, err := r.queries.SaveEvent(ctx, r.GetConnOrTx(), params)
	return row.Event, err
}

// LinkBatchActionChild inherits the parent's run in the child creation transaction.
func (r *PgEventRepo) LinkBatchActionChild(ctx common.ExtendedContext, parentID, eventID string) error {
	if r.Tx == nil {
		return errors.New("link batch action child requires a transaction")
	}
	if err := r.requireBatchActionAttempt(ctx, parentID); err != nil {
		return err
	}
	run, err := r.queries.LockBatchActionRunForEvent(ctx, r.Tx, parentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock batch action run for parent: %w", err)
	}
	// Check the parent after acquiring the run lock, in a fresh statement.
	processing, err := r.queries.BatchActionParentIsProcessing(ctx, r.Tx, parentID)
	if err != nil {
		return fmt.Errorf("check batch action parent status: %w", err)
	}
	if run.CompletedAt.Valid || !processing {
		return fmt.Errorf("batch action parent %s is no longer processing", parentID)
	}
	if err = r.queries.AddBatchActionRunEvent(ctx, r.Tx, AddBatchActionRunEventParams{EventID: eventID, RunID: run.ID}); err != nil {
		return fmt.Errorf("link batch action child: %w", err)
	}
	return nil
}

// FinishBatchActionRunIfIdle releases the reservation in the event completion transaction.
func (r *PgEventRepo) FinishBatchActionRunIfIdle(ctx common.ExtendedContext, eventID string) error {
	if r.Tx == nil {
		return errors.New("finish batch action run requires a transaction")
	}
	if err := r.queries.FinishBatchActionRunIfIdle(ctx, r.Tx, eventID); err != nil {
		return fmt.Errorf("finish batch action run: %w", err)
	}
	return nil
}

func (r *PgEventRepo) GetEvent(ctx common.ExtendedContext, id string) (Event, error) {
	row, err := r.queries.GetEvent(ctx, r.GetConnOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) GetEventForUpdate(ctx common.ExtendedContext, id string) (Event, error) {
	row, err := r.queries.GetEventForUpdate(ctx, r.GetConnOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) GetOlderIncompleteEvent(ctx common.ExtendedContext, event Event) (Event, error) {
	row, err := r.queries.GetOlderIncompleteEvent(ctx, r.GetConnOrTx(), GetOlderIncompleteEventParams{
		ID:               event.ID,
		EventType:        event.EventType,
		EventName:        event.EventName,
		Timestamp:        event.Timestamp,
		PatronRequestID:  event.PatronRequestID,
		IllTransactionID: event.IllTransactionID,
	})
	return row.Event, err
}

func (r *PgEventRepo) ClaimEventForSignal(ctx common.ExtendedContext, id string, signal Signal) (Event, error) {
	event, _, err := r.ClaimEventAttemptForSignal(ctx, id, signal)
	return event, err
}

func (r *PgEventRepo) claimEventSignal(ctx common.ExtendedContext, id string, signal Signal) (Event, error) {
	params := ClaimEventForSignalParams{
		ID:         id,
		LastSignal: string(signal),
	}
	row, err := r.queries.ClaimEventForSignal(ctx, r.GetConnOrTx(), params)
	return row.Event, err
}

func (r *PgEventRepo) UpdateEventLifecycle(ctx common.ExtendedContext, params UpdateEventLifecycleParams) (Event, error) {
	row, err := r.queries.UpdateEventLifecycle(ctx, r.GetConnOrTx(), params)
	return row.Event, err
}

func (r *PgEventRepo) Notify(ctx common.ExtendedContext, eventId string, signal Signal, target SignalTarget) error {
	data := NotifyData{Event: eventId, Signal: signal, Target: target}
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode event notification: %w", err)
	}
	if _, err = r.GetConnOrTx().Exec(ctx, "SELECT pg_notify($1, $2)", EVENT_BUS_CHANNEL, string(jsonData)); err != nil {
		return fmt.Errorf("publish event notification: %w", err)
	}
	return nil
}

func (r *PgEventRepo) GetIllTransactionEvents(ctx common.ExtendedContext, id string) ([]Event, int64, error) {
	rows, err := r.queries.GetIllTransactionEvents(ctx, r.GetConnOrTx(), id)
	var events []Event
	var fullCount int64
	if err == nil {
		for _, r := range rows {
			fullCount = r.FullCount
			events = append(events, r.Event)
		}
	}
	return events, fullCount, err
}

func (r *PgEventRepo) GetPatronRequestEvents(ctx common.ExtendedContext, id string) ([]Event, error) {
	rows, err := r.queries.GetPatronRequestEvents(ctx, r.GetConnOrTx(), id)
	var events []Event
	if err == nil {
		for _, r := range rows {
			events = append(events, r.Event)
		}
	}
	return events, err
}

func (r *PgEventRepo) GetBatchActionEvents(ctx common.ExtendedContext, taskID string) ([]Event, error) {
	rows, err := r.queries.GetBatchActionEvents(ctx, r.GetConnOrTx(), taskID)
	var eventList []Event
	if err == nil {
		for _, row := range rows {
			eventList = append(eventList, row.Event)
		}
	}
	return eventList, err
}

func (r *PgEventRepo) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (Event, error) {
	row, err := r.queries.GetLatestRequestEventByAction(ctx, r.GetConnOrTx(), GetLatestRequestEventByActionParams{
		Illtransactionid: illTransId,
		Action:           action,
	})
	return row.Event, err
}
