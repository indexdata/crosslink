package sched_service

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/stretchr/testify/assert"
)

type mockBatchActionEventBus struct {
	events.EventBus

	processCalled bool
	gotEvent      events.Event
	gotTarget     events.SignalTarget

	handlerStatus events.EventStatus
	handlerResult *events.EventResult

	processErr error
}

func (m *mockBatchActionEventBus) ProcessTask(
	ctx common.ExtendedContext,
	event events.Event,
	target events.SignalTarget,
	h func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult),
) (events.Event, error) {
	m.processCalled = true
	m.gotEvent = event
	m.gotTarget = target

	m.handlerStatus, m.handlerResult = h(ctx, event)

	return event, m.processErr
}

func batchActionEvent(actionName string) events.Event {
	return events.Event{
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{
					ActionName: actionName,
					Selector:   "cql.allRecords=1",
					TaskId:     "task-1",
				},
			},
		},
	}
}

func TestNewBatchActionService_WiresDependencies(t *testing.T) {
	eventBus := &mockBatchActionEventBus{}
	emailSender := EmailSenderServiceWithClient(nil, nil, nil, nil, false)

	svc := NewBatchActionService(eventBus, emailSender)

	assert.NotNil(t, svc)
	assert.Same(t, eventBus, svc.eventBus)
	assert.Same(t, emailSender, svc.emailSenderService)
}

func TestBatchAction_CallsProcessTaskWithSignalConsumers(t *testing.T) {
	eventBus := &mockBatchActionEventBus{}
	emailSender := EmailSenderServiceWithClient(nil, nil, nil, nil, false)
	svc := NewBatchActionService(eventBus, emailSender)

	event := events.Event{}

	svc.BatchAction(testCtx, event)

	assert.True(t, eventBus.processCalled)
	assert.Equal(t, event, eventBus.gotEvent)
	assert.Equal(t, events.SignalConsumers, eventBus.gotTarget)
	assert.Equal(t, events.EventStatusError, eventBus.handlerStatus)
	assert.NotNil(t, eventBus.handlerResult)
	assert.NotNil(t, eventBus.handlerResult.EventError)
	assert.Equal(t, "batch action data is empty", eventBus.handlerResult.EventError.Cause)
}

func TestBatchAction_ProcessTaskErrorIgnored(t *testing.T) {
	eventBus := &mockBatchActionEventBus{
		processErr: errors.New("event bus unavailable"),
	}
	emailSender := EmailSenderServiceWithClient(nil, nil, nil, nil, false)
	svc := NewBatchActionService(eventBus, emailSender)

	assert.NotPanics(t, func() {
		svc.BatchAction(testCtx, events.Event{})
	})

	assert.True(t, eventBus.processCalled)
}

func TestBatchAction_NilBatchActionDataReturnsError(t *testing.T) {
	svc := NewBatchActionService(nil, nil)

	status, result := svc.batchAction(testCtx, events.Event{})

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.NotNil(t, result.EventError)
	assert.Equal(t, "cannot process event", result.EventError.Message)
	assert.Equal(t, "batch action data is empty", result.EventError.Cause)
}

func TestBatchAction_UnknownActionReturnsError(t *testing.T) {
	svc := NewBatchActionService(nil, nil)

	event := batchActionEvent("unknown-action")

	status, result := svc.batchAction(testCtx, event)

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.NotNil(t, result.EventError)
	assert.Equal(t, "cannot process event", result.EventError.Message)
	assert.Equal(t, "unknown batch action", result.EventError.Cause)
}

func TestBatchAction_EmailPullslipsDispatchesToEmailSender(t *testing.T) {
	emailSender := EmailSenderServiceWithClient(nil, nil, nil, nil, false)
	svc := NewBatchActionService(nil, emailSender)

	event := batchActionEvent(string(schedoapi.BatchActionActionNameEmailPullslips))

	status, result := svc.batchAction(testCtx, event)

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.NotNil(t, result.EventError)
	assert.Equal(t, "email not sent", result.EventError.Message)
	assert.Equal(t, "email sending configuration missing", result.EventError.Cause)
}
