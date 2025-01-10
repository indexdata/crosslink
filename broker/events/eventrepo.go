package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/indexdata/crosslink/broker/repo"
)

type EventRepo interface {
	repo.Transactional[EventRepo]
	SaveEvent(params SaveEventParams) (Event, error)
	UpdateEventStatus(params UpdateEventStatusParams) error
	GetEvent(id string) (Event, error)
	Notify(eventId string, signal Signal) error
}

type PgEventRepo struct {
	repo.PgBaseRepo[EventRepo]
	queries Queries
}

// delegate transaction handling to Base
func (r *PgEventRepo) WithTxFunc(ctx context.Context, fn func(EventRepo) error) error {
	return r.PgBaseRepo.WithTxFunc(ctx, r, fn)
}

// DerivedRepo
func (r *PgEventRepo) CreateWithPgBaseRepo(base *repo.PgBaseRepo[EventRepo]) EventRepo {
	eventRepo := new(PgEventRepo)
	eventRepo.PgBaseRepo = *base
	return eventRepo
}

func (r *PgEventRepo) SaveEvent(params SaveEventParams) (Event, error) {
	row, err := r.queries.SaveEvent(context.Background(), r.GetConnOrTx(), params)
	return row.Event, err
}

func (r *PgEventRepo) GetEvent(id string) (Event, error) {
	row, err := r.queries.GetEvent(context.Background(), r.GetConnOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) UpdateEventStatus(params UpdateEventStatusParams) error {
	return r.queries.UpdateEventStatus(context.Background(), r.GetConnOrTx(), params)
}

func (r *PgEventRepo) Notify(eventId string, signal Signal) error {
	data := NotifyData{
		Event:  eventId,
		Signal: signal,
	}
	jsonData, _ := json.Marshal(data)
	sql := fmt.Sprintf("NOTIFY crosslink_channel, '%s'", jsonData)
	_, err := r.GetConnOrTx().Exec(context.Background(), sql)
	return err
}
