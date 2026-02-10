package events

import (
	"encoding/json"
	"fmt"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
)

type EventRepo interface {
	repo.Transactional[EventRepo]
	SaveEvent(ctx common.ExtendedContext, params SaveEventParams) (Event, error)
	UpdateEventStatus(ctx common.ExtendedContext, params UpdateEventStatusParams) (Event, error)
	GetEvent(ctx common.ExtendedContext, id string) (Event, error)
	GetEventForUpdate(ctx common.ExtendedContext, id string) (Event, error)
	ClaimEventForSignal(ctx common.ExtendedContext, id string, signal Signal) (Event, error)
	Notify(ctx common.ExtendedContext, eventId string, signal Signal) error
	GetIllTransactionEvents(ctx common.ExtendedContext, id string) ([]Event, int64, error)
	DeleteEventsByIllTransaction(ctx common.ExtendedContext, illTransId string) error
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

func (r *PgEventRepo) GetEvent(ctx common.ExtendedContext, id string) (Event, error) {
	row, err := r.queries.GetEvent(ctx, r.GetConnOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) GetEventForUpdate(ctx common.ExtendedContext, id string) (Event, error) {
	row, err := r.queries.GetEventForUpdate(ctx, r.GetConnOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) ClaimEventForSignal(ctx common.ExtendedContext, id string, signal Signal) (Event, error) {
	params := ClaimEventForSignalParams{
		ID:         id,
		LastSignal: string(signal),
	}
	row, err := r.queries.ClaimEventForSignal(ctx, r.GetConnOrTx(), params)
	return row.Event, err
}

func (r *PgEventRepo) UpdateEventStatus(ctx common.ExtendedContext, params UpdateEventStatusParams) (Event, error) {
	row, err := r.queries.UpdateEventStatus(ctx, r.GetConnOrTx(), params)
	return row.Event, err
}

func (r *PgEventRepo) Notify(ctx common.ExtendedContext, eventId string, signal Signal) error {
	data := NotifyData{
		Event:  eventId,
		Signal: signal,
	}
	jsonData, _ := json.Marshal(data)
	sql := fmt.Sprintf("NOTIFY crosslink_channel, '%s'", jsonData)
	_, err := r.GetConnOrTx().Exec(ctx, sql)
	return err
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

func (r *PgEventRepo) DeleteEventsByIllTransaction(ctx common.ExtendedContext, illTransId string) error {
	return r.queries.DeleteEventsByIllTransaction(ctx, r.GetConnOrTx(), illTransId)
}

func (r *PgEventRepo) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (Event, error) {
	row, err := r.queries.GetLatestRequestEventByAction(ctx, r.GetConnOrTx(), GetLatestRequestEventByActionParams{
		Illtransactionid: illTransId,
		Action:           action,
	})
	return row.Event, err
}
