package sched_service

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/stretchr/testify/assert"
)

const testBatchActionOwner = "ISIL:TEST"

type createTaskCall struct {
	id          string
	eventName   events.EventName
	data        events.EventData
	eventDomain events.EventDomain
	parentID    *string
	target      events.SignalTarget
}

type mockBatchActionEventBus struct {
	events.EventBus

	processCalled bool
	gotEvent      events.Event
	gotTarget     events.SignalTarget

	handlerStatus events.EventStatus
	handlerResult *events.EventResult

	processErr        error
	createTaskCalls   []createTaskCall
	createTaskErr     error
	createTaskErrByID map[string]error
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

func (m *mockBatchActionEventBus) CreateTask(id string, eventName events.EventName, data events.EventData, eventDomain events.EventDomain, parentID *string, target events.SignalTarget) (string, error) {
	m.createTaskCalls = append(m.createTaskCalls, createTaskCall{
		id:          id,
		eventName:   eventName,
		data:        data,
		eventDomain: eventDomain,
		parentID:    parentID,
		target:      target,
	})
	if err := m.createTaskErrByID[id]; err != nil {
		return "", err
	}
	if m.createTaskErr != nil {
		return "", m.createTaskErr
	}
	return "event-" + id, nil
}

func batchActionEvent(actionName string) events.Event {
	return events.Event{
		ID: "batch-event-1",
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{
					ActionName: actionName,
					Selector:   "cql.allRecords=1",
					TaskId:     "task-1",
					Owner:      testBatchActionOwner,
				},
			},
		},
	}
}

func requestAgingEvent(selector string, customData map[string]any) events.Event {
	event := batchActionEvent(string(schedoapi.RequestAging))
	event.EventData.BatchActionData.Selector = selector
	event.EventData.CustomData = customData
	return event
}

func TestNewBatchActionService_WiresDependencies(t *testing.T) {
	eventBus := &mockBatchActionEventBus{}
	emailSender := EmailSenderServiceWithClient(nil, nil, nil, nil)

	svc := NewBatchActionService(eventBus, &mockEmailPrRepo{}, emailSender)

	assert.NotNil(t, svc)
	assert.Same(t, eventBus, svc.eventBus)
	assert.Same(t, emailSender, svc.emailSenderService)
}

func TestBatchAction_CallsProcessTaskWithSignalConsumers(t *testing.T) {
	eventBus := &mockBatchActionEventBus{}
	emailSender := EmailSenderServiceWithClient(nil, nil, nil, nil)
	svc := NewBatchActionService(eventBus, &mockEmailPrRepo{}, emailSender)

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
	emailSender := EmailSenderServiceWithClient(nil, nil, nil, nil)
	svc := NewBatchActionService(eventBus, &mockEmailPrRepo{}, emailSender)

	assert.NotPanics(t, func() {
		svc.BatchAction(testCtx, events.Event{})
	})

	assert.True(t, eventBus.processCalled)
}

func TestBatchAction_NilBatchActionDataReturnsError(t *testing.T) {
	svc := NewBatchActionService(nil, &mockEmailPrRepo{}, nil)

	status, result := svc.batchAction(testCtx, events.Event{})

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.NotNil(t, result.EventError)
	assert.Equal(t, "cannot process event", result.EventError.Message)
	assert.Equal(t, "batch action data is empty", result.EventError.Cause)
}

func TestBatchAction_UnknownActionReturnsError(t *testing.T) {
	svc := NewBatchActionService(nil, &mockEmailPrRepo{}, nil)

	event := batchActionEvent("unknown-action")

	status, result := svc.batchAction(testCtx, event)

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.NotNil(t, result.EventError)
	assert.Equal(t, "cannot process event", result.EventError.Message)
	assert.Equal(t, "unknown batch action", result.EventError.Cause)
}

func TestBatchAction_EmailPullslipsDispatchesToEmailSender(t *testing.T) {
	emailSender := EmailSenderServiceWithClient(nil, nil, &mockEmailService{ready: false}, nil)
	svc := NewBatchActionService(nil, &mockEmailPrRepo{}, emailSender)

	event := batchActionEvent(string(schedoapi.EmailPullslips))

	status, result := svc.batchAction(testCtx, event)

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.NotNil(t, result.EventError)
	assert.Equal(t, "email not sent", result.EventError.Message)
	assert.Equal(t, "email sending configuration missing", result.EventError.Cause)
}

func TestBatchAction_RequestAgingDispatches(t *testing.T) {
	repo := &mockEmailPrRepo{}
	svc := NewBatchActionService(&mockBatchActionEventBus{}, repo, nil)

	status, result := svc.batchAction(testCtx, requestAgingEvent("cql.allRecords=1", map[string]any{"interval": "24h"}))

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, result)
	assert.Equal(t, "processed patron request count: 0", result.Note)
	assert.True(t, repo.listCalled)
}

func TestBatchAction_AddsOwnerRestrictionBeforeDispatch(t *testing.T) {
	repo := &mockEmailPrRepo{}
	svc := NewBatchActionService(&mockBatchActionEventBus{}, repo, nil)
	event := requestAgingEvent("state = REQ", map[string]any{"interval": "24h"})

	status, _ := svc.batchAction(testCtx, event)

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t,
		"(state = $3 AND ((side = $4 AND supplier_symbol = $5) OR (side = $6 AND requester_symbol = $7))) AND updated_at <= $8",
		repo.gotQuery.GetWhereClause(),
	)
	// The restriction is added to the dispatched copy, not persisted into the
	// scheduled event payload where repeated runs could accumulate predicates.
	assert.Equal(t, "state = REQ", event.EventData.BatchActionData.Selector)
}

func TestBatchAction_MissingOwnerDispatchesWithoutRestriction(t *testing.T) {
	repo := &mockEmailPrRepo{}
	svc := NewBatchActionService(&mockBatchActionEventBus{}, repo, nil)
	event := requestAgingEvent("state = REQ", map[string]any{"interval": "24h"})
	event.EventData.BatchActionData.Owner = ""

	status, result := svc.batchAction(testCtx, event)

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, result.EventError)
	assert.True(t, repo.listCalled)
	assert.Equal(t, "state = $3 AND updated_at <= $4", repo.gotQuery.GetWhereClause())
}

func TestRequestAging_ValidationErrors(t *testing.T) {
	tests := []struct {
		name              string
		event             events.Event
		wantCause         string
		wantCauseContains string
		wantMsg           string
	}{
		{
			name:      "nil batch action data",
			event:     events.Event{},
			wantMsg:   "cannot process event",
			wantCause: "batch action data is empty",
		},
		{
			name:      "empty selector",
			event:     requestAgingEvent("", map[string]any{"interval": "24h"}),
			wantMsg:   "cannot process event",
			wantCause: "selector is empty",
		},
		{
			name:      "missing custom data",
			event:     requestAgingEvent("cql.allRecords=1", nil),
			wantMsg:   "cannot process event",
			wantCause: "interval is missing or not a string",
		},
		{
			name:      "missing interval",
			event:     requestAgingEvent("cql.allRecords=1", map[string]any{}),
			wantMsg:   "cannot process event",
			wantCause: "interval is missing or not a string",
		},
		{
			name:      "non-string interval",
			event:     requestAgingEvent("cql.allRecords=1", map[string]any{"interval": 24}),
			wantMsg:   "cannot process event",
			wantCause: "interval is missing or not a string",
		},
		{
			name:      "empty interval",
			event:     requestAgingEvent("cql.allRecords=1", map[string]any{"interval": ""}),
			wantMsg:   "cannot process event",
			wantCause: "interval is missing or not a string",
		},
		{
			name:      "invalid interval",
			event:     requestAgingEvent("cql.allRecords=1", map[string]any{"interval": "not-a-duration"}),
			wantMsg:   "cannot process event",
			wantCause: "interval is invalid",
		},
		{
			name:              "invalid selector",
			event:             requestAgingEvent("side =", map[string]any{"interval": "24h"}),
			wantMsg:           "invalid cql selector",
			wantCauseContains: "search term expected",
		},
		{
			name:      "unsupported selector field",
			event:     requestAgingEvent("unsupported_field = value", map[string]any{"interval": "24h"}),
			wantMsg:   "invalid cql selector",
			wantCause: "unknown field unsupported_field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockEmailPrRepo{}
			eventBus := &mockBatchActionEventBus{}
			svc := NewBatchActionService(eventBus, repo, nil)

			status, result := svc.RequestAging(testCtx, tt.event)

			assert.Equal(t, events.EventStatusError, status)
			assert.NotNil(t, result)
			assert.NotNil(t, result.EventError)
			assert.Equal(t, tt.wantMsg, result.EventError.Message)
			if tt.wantCauseContains != "" {
				assert.Contains(t, result.EventError.Cause, tt.wantCauseContains)
			} else {
				assert.Equal(t, tt.wantCause, result.EventError.Cause)
			}
			assert.False(t, repo.listCalled)
			assert.Empty(t, eventBus.createTaskCalls)
		})
	}
}

func TestRequestAging_NoPatronRequestsReturnsSuccess(t *testing.T) {
	repo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{}}
	eventBus := &mockBatchActionEventBus{}
	svc := NewBatchActionService(eventBus, repo, nil)

	status, result := svc.RequestAging(testCtx, requestAgingEvent("cql.allRecords=1", map[string]any{"interval": "24h"}))

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, result)
	assert.Equal(t, "processed patron request count: 0", result.Note)
	assert.True(t, repo.listCalled)
	assert.Empty(t, eventBus.createTaskCalls)
}

func TestRequestAging_CreatesBackgroundTasksForLending(t *testing.T) {
	repo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{
		{ID: "lending-1", Side: prservice.SideLending, State: prservice.LenderStateValidated},
		{ID: "lending-2", Side: prservice.SideLending, State: prservice.LenderStateWillSupply},
	}}
	eventBus := &mockBatchActionEventBus{}
	svc := NewBatchActionService(eventBus, repo, nil)

	status, result := svc.RequestAging(testCtx, requestAgingEvent("cql.allRecords=1", map[string]any{"interval": "24h", "note": "Closing stale request", "reasonUnfilled": "Expired"}))

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, result)
	assert.Equal(t, "processed patron request count: 2", result.Note)
	if assert.Len(t, eventBus.createTaskCalls, 2) {
		assertRequestAgingCreateTask(t, eventBus.createTaskCalls[0], "lending-1", prservice.LenderActionCannotSupply)
		assertRequestAgingCreateTask(t, eventBus.createTaskCalls[1], "lending-2", prservice.LenderActionCannotSupply)
		for _, call := range eventBus.createTaskCalls {
			assert.Equal(t, "Closing stale request", call.data.CustomData["note"])
			assert.Equal(t, "Expired", call.data.CustomData["reasonUnfilled"])
			_, hasInterval := call.data.CustomData["interval"]
			assert.False(t, hasInterval)
		}
	}
}

func TestRequestAging_FailsIfNoClosingActionForState(t *testing.T) {
	repo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{
		{ID: "lending-1", Side: prservice.SideLending, State: prservice.LenderStateShipped},
		{ID: "lending-2", Side: prservice.SideLending, State: prservice.LenderStateWillSupply},
	}}
	eventBus := &mockBatchActionEventBus{}
	svc := NewBatchActionService(eventBus, repo, nil)

	status, result := svc.RequestAging(testCtx, requestAgingEvent("cql.allRecords=1", map[string]any{"interval": "24h", "note": "Closing stale request", "reasonUnfilled": "Expired"}))

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.Equal(t, "processed patron request count: 2, failed: 1 with ids and errors in custom data", result.Note)
	assert.Equal(t, "could not find closing action for patron request state: SHIPPED within state model: CrossLink Returnables State Model", result.CustomData["lending-1"])
	assert.Len(t, eventBus.createTaskCalls, 1)
}

func TestRequestAging_CreateTaskErrorRecordsCustomDataAndContinues(t *testing.T) {
	repo := &mockEmailPrRepo{listResult: []pr_db.PatronRequest{
		{ID: "failed-1", Side: prservice.SideLending, State: prservice.LenderStateValidated},
		{ID: "ok-1", Side: prservice.SideLending, State: prservice.LenderStateWillSupply},
	}}
	eventBus := &mockBatchActionEventBus{createTaskErrByID: map[string]error{"failed-1": errors.New("create failed")}}
	svc := NewBatchActionService(eventBus, repo, nil)

	status, result := svc.RequestAging(testCtx, requestAgingEvent("cql.allRecords=1", map[string]any{"interval": "24h"}))

	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, result)
	assert.Equal(t, "processed patron request count: 2, failed: 1 with ids and errors in custom data", result.Note)
	assert.Equal(t, "error creating close action: create failed", result.CustomData["failed-1"])
	_, ok := result.CustomData["ok-1"]
	assert.False(t, ok)
	assert.Len(t, eventBus.createTaskCalls, 2)
}

func assertRequestAgingCreateTask(t *testing.T, got createTaskCall, wantID string, wantAction pr_db.PatronRequestAction) {
	t.Helper()
	assert.Equal(t, wantID, got.id)
	assert.Equal(t, events.EventNameInvokeBackgroundAction, got.eventName)
	assert.Equal(t, events.EventDomainPatronRequest, got.eventDomain)
	if assert.NotNil(t, got.parentID) {
		assert.Equal(t, "batch-event-1", *got.parentID)
	}
	assert.Equal(t, events.SignalConsumers, got.target)
	if assert.NotNil(t, got.data.Action) {
		assert.Equal(t, wantAction, *got.data.Action)
	}
	if assert.NotNil(t, got.data.BatchActionData) {
		assert.Equal(t, events.BatchActionData{
			ActionName: string(schedoapi.RequestAging),
			Selector:   "cql.allRecords=1",
			TaskId:     "task-1",
			Owner:      testBatchActionOwner,
		}, *got.data.BatchActionData)
	}
}
