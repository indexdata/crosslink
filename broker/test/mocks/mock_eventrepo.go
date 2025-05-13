package mocks

import (
	"errors"

	"github.com/google/uuid"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/stretchr/testify/mock"
)

type MockEventRepositorySuccess struct {
	mock.Mock
}

func (r *MockEventRepositorySuccess) WithTxFunc(ctx extctx.ExtendedContext, fn func(events.EventRepo) error) error {
	return nil
}

func (r *MockEventRepositorySuccess) SaveEvent(ctx extctx.ExtendedContext, params events.SaveEventParams) (events.Event, error) {
	var event = (events.Event)(params)
	return event, nil
}

func (r *MockEventRepositorySuccess) UpdateEventStatus(ctx extctx.ExtendedContext, params events.UpdateEventStatusParams) error {
	return nil
}

func (r *MockEventRepositorySuccess) GetEvent(ctx extctx.ExtendedContext, id string) (events.Event, error) {
	if id == "t-1-n" {
		return events.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        test.GetNow(),
			EventType:        events.EventTypeTask,
			EventName:        events.EventNameRequestReceived,
			EventStatus:      events.EventStatusNew,
		}, nil
	} else if id == "t-1-p" {
		return events.Event{
			ID:               id,
			IllTransactionID: uuid.New().String(),
			Timestamp:        test.GetNow(),
			EventType:        events.EventTypeTask,
			EventName:        events.EventNameRequestReceived,
			EventStatus:      events.EventStatusProcessing,
		}, nil
	} else {
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

func (r *MockEventRepositorySuccess) Notify(ctx extctx.ExtendedContext, eventId string, signal events.Signal) error {
	return nil
}

func (r *MockEventRepositorySuccess) GetIllTransactionEvents(ctx extctx.ExtendedContext, id string) ([]events.Event, int64, error) {
	return []events.Event{{
		ID: uuid.New().String(),
	}}, 0, nil
}

func (r *MockEventRepositorySuccess) DeleteEventsByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error {
	return nil
}

type MockEventRepositoryError struct {
	mock.Mock
}

func (r *MockEventRepositoryError) WithTxFunc(ctx extctx.ExtendedContext, fn func(events.EventRepo) error) error {
	return nil
}

func (r *MockEventRepositoryError) SaveEvent(ctx extctx.ExtendedContext, params events.SaveEventParams) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}

func (r *MockEventRepositoryError) GetEvent(ctx extctx.ExtendedContext, id string) (events.Event, error) {
	return events.Event{}, errors.New("DB error")
}

func (r *MockEventRepositoryError) UpdateEventStatus(ctx extctx.ExtendedContext, params events.UpdateEventStatusParams) error {
	return nil
}

func (r *MockEventRepositoryError) Notify(ctx extctx.ExtendedContext, eventId string, signal events.Signal) error {
	return errors.New("DB error")
}

func (r *MockEventRepositoryError) GetIllTransactionEvents(ctx extctx.ExtendedContext, id string) ([]events.Event, int64, error) {
	return []events.Event{}, 0, errors.New("DB error")
}

func (r *MockEventRepositoryError) DeleteEventsByIllTransaction(ctx extctx.ExtendedContext, illTransId string) error {
	return errors.New("DB error")
}
