package mocks

import (
	"errors"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/stretchr/testify/mock"
)

type MockEventRepositorySuccess struct {
	mock.Mock
}

func (r *MockEventRepositorySuccess) WithTxFunc(ctx common.ExtendedContext, fn func(events.EventRepo) error) error {
	return nil
}

func (r *MockEventRepositorySuccess) SaveEvent(ctx common.ExtendedContext, params events.SaveEventParams) (events.Event, error) {
	var event = (events.Event)(params)
	return event, nil
}

func (r *MockEventRepositorySuccess) UpdateEventStatus(ctx common.ExtendedContext, params events.UpdateEventStatusParams) (events.Event, error) {
	return events.Event{}, nil
}

func (r *MockEventRepositorySuccess) GetEvent(ctx common.ExtendedContext, id string) (events.Event, error) {
	switch id {
	case "t-1-n":
		return events.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        test.GetNow(),
			EventType:        events.EventTypeTask,
			EventName:        events.EventNameRequestReceived,
			EventStatus:      events.EventStatusNew,
		}, nil
	case "t-1-p":
		return events.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        test.GetNow(),
			EventType:        events.EventTypeTask,
			EventName:        events.EventNameRequestReceived,
			EventStatus:      events.EventStatusProcessing,
		}, nil
	default:
		return events.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        test.GetNow(),
			EventType:        events.EventTypeNotice,
			EventName:        events.EventNameRequesterMsgReceived,
			EventStatus:      events.EventStatusSuccess,
		}, nil
	}
}

func (r *MockEventRepositorySuccess) GetEventForUpdate(ctx common.ExtendedContext, id string) (events.Event, error) {
	return r.GetEvent(ctx, id)
}

func (r *MockEventRepositorySuccess) ClaimEventForSignal(ctx common.ExtendedContext, id string, signal events.Signal) (events.Event, error) {
	return r.GetEvent(ctx, id)
}

func (r *MockEventRepositorySuccess) Notify(ctx common.ExtendedContext, eventId string, signal events.Signal) error {
	return nil
}

func (r *MockEventRepositorySuccess) GetIllTransactionEvents(ctx common.ExtendedContext, id string) ([]events.Event, int64, error) {
	return []events.Event{{
		ID: uuid.New().String(),
	}}, 0, nil
}

func (r *MockEventRepositorySuccess) DeleteEventsByIllTransaction(ctx common.ExtendedContext, illTransId string) error {
	return nil
}

func (r *MockEventRepositorySuccess) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (events.Event, error) {
	return events.Event{
		ID:               uuid.New().String(),
		IllTransactionID: illTransId,
	}, nil
}

type MockEventRepositoryError struct {
	mock.Mock
}

func (r *MockEventRepositoryError) WithTxFunc(ctx common.ExtendedContext, fn func(events.EventRepo) error) error {
	return nil
}

func (r *MockEventRepositoryError) SaveEvent(ctx common.ExtendedContext, params events.SaveEventParams) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}

func (r *MockEventRepositoryError) GetEvent(ctx common.ExtendedContext, id string) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}

func (r *MockEventRepositoryError) GetEventForUpdate(ctx common.ExtendedContext, id string) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}

func (r *MockEventRepositoryError) ClaimEventForSignal(ctx common.ExtendedContext, id string, signal events.Signal) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}

func (r *MockEventRepositoryError) UpdateEventStatus(ctx common.ExtendedContext, params events.UpdateEventStatusParams) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}

func (r *MockEventRepositoryError) Notify(ctx common.ExtendedContext, eventId string, signal events.Signal) error {
	return errors.New("DB error")
}

func (r *MockEventRepositoryError) GetIllTransactionEvents(ctx common.ExtendedContext, id string) ([]events.Event, int64, error) {
	return []events.Event{}, 0, errors.New("DB error")
}

func (r *MockEventRepositoryError) DeleteEventsByIllTransaction(ctx common.ExtendedContext, illTransId string) error {
	return errors.New("DB error")
}

func (r *MockEventRepositoryError) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}
