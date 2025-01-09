package events

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/indexdata/crosslink/broker/repo"
)

type EventRepo interface {
	repo.BaseRepo
	repo.DerivedRepo
	SaveEvent(params SaveEventParams) (Event, error)
	UpdateEventStatus(params UpdateEventStatusParams) error
	GetEvent(id string) (Event, error)
	Notify(eventId string, signal Signal) error
}

type PgEventRepo struct {
	repo.PgBaseRepo
	queries Queries
}

func (r *PgEventRepo) CreateWithBaseRepo(base repo.BaseRepo) repo.DerivedRepo {
	eventRepo := new(PgEventRepo)
	eventRepo.PgBaseRepo = *base.(*repo.PgBaseRepo)
	return eventRepo
}

func (r *PgEventRepo) SaveEvent(params SaveEventParams) (Event, error) {
	row, err := r.queries.SaveEvent(context.Background(), r.GetPoolOrTx(), params)
	return row.Event, err
}

func (r *PgEventRepo) GetEvent(id string) (Event, error) {
	row, err := r.queries.GetEvent(context.Background(), r.GetPoolOrTx(), id)
	return row.Event, err
}

func (r *PgEventRepo) UpdateEventStatus(params UpdateEventStatusParams) error {
	return r.queries.UpdateEventStatus(context.Background(), r.GetPoolOrTx(), params)
}

func (r *PgEventRepo) Notify(eventId string, signal Signal) error {
	data := NotifyData{
		Event:  eventId,
		Signal: signal,
	}
	jsonData, _ := json.Marshal(data)
	sql := fmt.Sprintf("NOTIFY crosslink_channel, '%s'", jsonData)
	_, err := r.GetPoolOrTx().Exec(context.Background(), sql)
	return err
}
