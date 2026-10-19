package events

import (
	"context"
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestProcessExclusiveTaskCompletesTaskWhenExclusivityCheckFails(t *testing.T) {
	repo := &exclusiveCheckErrorRepo{
		event: Event{
			ID:               "event-1",
			IllTransactionID: DEFAULT_ILL_TRANSACTION_ID,
			PatronRequestID:  "pr-1",
			Timestamp:        pgtype.Timestamp{Valid: true},
			EventType:        EventTypeTask,
			EventName:        EventNameInvokeAction,
			EventStatus:      EventStatusNew,
		},
		checkErr: errors.New("DB error"),
	}
	eventBus := NewPostgresEventBus(repo, "")
	eventBus.ctx = common.CreateExtCtxWithArgs(context.Background(), nil)

	handlerCalled := false
	event, err := eventBus.ProcessExclusiveTask(eventBus.ctx, Event{ID: repo.event.ID}, SignalConsumers, func(common.ExtendedContext, Event) (EventStatus, *EventResult) {
		handlerCalled = true
		return EventStatusSuccess, &EventResult{}
	})

	assert.Error(t, err)
	assert.Equal(t, "DB error", err.Error())
	assert.False(t, handlerCalled)
	assert.Equal(t, EventStatusError, event.EventStatus)
	if assert.NotNil(t, event.ResultData.EventError) {
		assert.Equal(t, "failed to check exclusive task", event.ResultData.EventError.Message)
		assert.Equal(t, "DB error", event.ResultData.EventError.Cause)
	}
}

type exclusiveCheckErrorRepo struct {
	event    Event
	checkErr error
}

func (r *exclusiveCheckErrorRepo) WithTxFunc(ctx common.ExtendedContext, fn func(EventRepo) error) error {
	return fn(r)
}

func (r *exclusiveCheckErrorRepo) SaveEvent(ctx common.ExtendedContext, params SaveEventParams) (Event, error) {
	r.event = Event(params)
	return r.event, nil
}

func (r *exclusiveCheckErrorRepo) UpdateEventLifecycle(ctx common.ExtendedContext, params UpdateEventLifecycleParams) (Event, error) {
	r.event.EventStatus = params.EventStatus
	r.event.LastSignal = params.LastSignal
	return r.event, nil
}

func (r *exclusiveCheckErrorRepo) GetEvent(ctx common.ExtendedContext, id string) (Event, error) {
	return r.event, nil
}

func (r *exclusiveCheckErrorRepo) GetEventForUpdate(ctx common.ExtendedContext, id string) (Event, error) {
	return r.event, nil
}

func (r *exclusiveCheckErrorRepo) GetOlderIncompleteEvent(ctx common.ExtendedContext, event Event) (Event, error) {
	return Event{}, r.checkErr
}

func (r *exclusiveCheckErrorRepo) ClaimEventForSignal(ctx common.ExtendedContext, id string, signal Signal) (Event, error) {
	return Event{}, nil
}

func (r *exclusiveCheckErrorRepo) Notify(ctx common.ExtendedContext, eventId string, signal Signal, target SignalTarget) error {
	return nil
}

func (r *exclusiveCheckErrorRepo) GetIllTransactionEvents(ctx common.ExtendedContext, id string) ([]Event, int64, error) {
	return nil, 0, nil
}

func (r *exclusiveCheckErrorRepo) GetBatchActionEvents(ctx common.ExtendedContext, taskID string) ([]Event, error) {
	return nil, nil
}

func (r *exclusiveCheckErrorRepo) GetLatestRequestEventByAction(ctx common.ExtendedContext, illTransId string, action string) (Event, error) {
	return Event{}, nil
}

func (r *exclusiveCheckErrorRepo) GetPatronRequestEvents(ctx common.ExtendedContext, id string) ([]Event, error) {
	return nil, nil
}

func TestCreateNoticeWithID(t *testing.T) {
	repo := &noticeIDRepo{}
	bus := NewPostgresEventBus(repo, "")
	bus.ctx = common.CreateExtCtxWithArgs(context.Background(), nil)
	id, err := bus.CreateNoticeWithID("allocated-event", "transaction", EventNameRequesterMsgReceived, EventData{}, EventStatusSuccess, EventDomainIllTransaction, SignalConsumers)
	assert.NoError(t, err)
	assert.Equal(t, "allocated-event", id)
	assert.Equal(t, id, repo.saved.ID)
	assert.Equal(t, "transaction", repo.saved.IllTransactionID)
	assert.Equal(t, EventTypeNotice, repo.saved.EventType)
	assert.Equal(t, EventNameRequesterMsgReceived, repo.saved.EventName)
	assert.Equal(t, EventStatusSuccess, repo.saved.EventStatus)
	assert.Equal(t, id, repo.notifiedID)
	assert.Equal(t, SignalNoticeCreated, repo.signal)
	assert.Equal(t, SignalConsumers, repo.target)
}

type noticeIDRepo struct {
	EventRepo
	saved      Event
	notifiedID string
	signal     Signal
	target     SignalTarget
}

func (r *noticeIDRepo) WithTxFunc(_ common.ExtendedContext, fn func(EventRepo) error) error {
	return fn(r)
}
func (r *noticeIDRepo) SaveEvent(_ common.ExtendedContext, params SaveEventParams) (Event, error) {
	r.saved = Event(params)
	return r.saved, nil
}
func (r *noticeIDRepo) Notify(_ common.ExtendedContext, id string, signal Signal, target SignalTarget) error {
	r.notifiedID, r.signal, r.target = id, signal, target
	return nil
}
