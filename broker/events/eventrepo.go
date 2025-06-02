package events

import (
	"encoding/json"
	"fmt"

	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/repo"
)

type EventRepo interface {
	repo.Transactional[EventRepo]
	SaveEvent(ctx extctx.ExtendedContext, params SaveEventParams) (Event, error)
	UpdateEventStatus(ctx extctx.ExtendedContext, params UpdateEventStatusParams) error
	GetEvent(ctx extctx.ExtendedContext, id string) (Event, error)
	GetNewEvent(ctx extctx.ExtendedContext, id string) (Event, error)
	Notify(ctx extctx.ExtendedContext, eventId string, signal Signal) error
	GetIllTransactionEvents(ctx extctx.ExtendedContext, id string) ([]Event, int64, error)
	DeleteEventsByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error
}

type PgEventRepo struct {
	repo.PgBaseRepo[EventRepo]
	queries Queries
}

// delegate transaction handling to Base
func (r *PgEventRepo) WithTxFunc(ctx extctx.ExtendedContext, fn func(EventRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// DerivedRepo
func (r *PgEventRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[EventRepo]) EventRepo {
	eventRepo := new(PgEventRepo)
	eventRepo.PgBaseRepo = *base
	return eventRepo
}

func (r *PgEventRepo) SaveEvent(ctx extctx.ExtendedContext, params SaveEventParams) (Event, error) {
	row, err := r.queries.SaveEvent(ctx, r.GetConnOrTx(), params)
	return row.Event, err
}

func (r *PgEventRepo) GetEvent(ctx extctx.ExtendedContext, id string) (Event, error) {
	row, err := r.queries.GetEvent(ctx, r.GetConnOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) GetNewEvent(ctx extctx.ExtendedContext, id string) (Event, error) {
	row, err := r.queries.GetNewEvent(ctx, r.GetConnOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) UpdateEventStatus(ctx extctx.ExtendedContext, params UpdateEventStatusParams) error {
	return r.queries.UpdateEventStatus(ctx, r.GetConnOrTx(), params)
}

func (r *PgEventRepo) Notify(ctx extctx.ExtendedContext, eventId string, signal Signal) error {
	data := NotifyData{
		Event:  eventId,
		Signal: signal,
	}
	jsonData, _ := json.Marshal(data)
	sql := fmt.Sprintf("NOTIFY crosslink_channel, '%s'", jsonData)
	_, err := r.GetConnOrTx().Exec(ctx, sql)
	return err
}

func (r *PgEventRepo) GetIllTransactionEvents(ctx extctx.ExtendedContext, id string) ([]Event, int64, error) {
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

func (r *PgEventRepo) DeleteEventsByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error {
	return r.queries.DeleteEventsByIllTransaction(ctx, r.GetConnOrTx(), illTransId)
}
