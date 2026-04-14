package prservice

import (
	"errors"
	"testing"

	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

func TestSendPatronRequestNotificationSuccess(t *testing.T) {
	pr := pr_db.PatronRequest{ID: "1"}
	notification := pr_db.Notification{ID: "n1"}
	mockEventBus := new(MockEventBus)
	service := CreatePatronRequestNotificationService(new(MockPrRepo), mockEventBus, new(MockIso18626Handler))

	err := service.SendPatronRequestNotification(appCtx, pr, notification)
	assert.NoError(t, err)
	mockEventBus.AssertExpectations(t)
}

func TestSendPatronRequestNotificationErrorEventCreation(t *testing.T) {
	pr := pr_db.PatronRequest{ID: "error"}
	notification := pr_db.Notification{ID: "n1"}
	mockEventBus := new(MockEventBus)
	service := CreatePatronRequestNotificationService(new(MockPrRepo), mockEventBus, new(MockIso18626Handler))

	err := service.SendPatronRequestNotification(appCtx, pr, notification)
	assert.Equal(t, "failed to create event for patron request notification(n1): event bus error", err.Error())
}

func TestSendPatronRequestNotificationErrorEventProcess(t *testing.T) {
	pr := pr_db.PatronRequest{ID: "1"}
	notification := pr_db.Notification{ID: "n1"}
	mockEventBus := new(MockEventBus)
	event := events.Event{
		ID: "1-task-1",
	}
	mockEventBus.On("ProcessTask", event.ID).Return(event, errors.New("processing error"))
	service := CreatePatronRequestNotificationService(new(MockPrRepo), mockEventBus, new(MockIso18626Handler))

	err := service.SendPatronRequestNotification(appCtx, pr, notification)
	assert.Equal(t, "failed to process event for patron request notification(n1): processing error", err.Error())
}

func TestHandleInvokeNotificationParseError(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "1",
		EventData:       events.EventData{CustomData: map[string]any{"notification": "n1"}},
	}
	service := CreatePatronRequestNotificationService(new(MockPrRepo), new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "invalid event data: missing notification", result.EventError.Message)
}

func TestHandleInvokeNotificationParseErrorId(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "1",
		EventData:       events.EventData{CustomData: map[string]any{"notification": map[string]any{"ID": 1}}},
	}
	service := CreatePatronRequestNotificationService(new(MockPrRepo), new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "invalid event data: missing id", result.EventError.Message)
}

func TestHandleInvokeNotificationReadPrError(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "1",
		EventData:       events.EventData{CustomData: map[string]any{"notification": map[string]any{"ID": "1"}}},
	}
	service := CreatePatronRequestNotificationService(new(MockPrRepo), new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to read patron request", result.EventError.Message)
}

func TestHandleInvokeNotificationReadNotificationError(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "2",
		EventData:       events.EventData{CustomData: map[string]any{"notification": map[string]any{"ID": "n1"}}},
	}
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", "2").Return(pr_db.PatronRequest{ID: patronRequestId, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("req-1")}, nil)
	mockPrRepo.On("GetNotificationById", "n1").Return(pr_db.Notification{}, errors.New("db error"))
	service := CreatePatronRequestNotificationService(mockPrRepo, new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to read notification", result.EventError.Message)
}

func TestHandleInvokeNotificationSuccess(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "2",
		EventData:       events.EventData{CustomData: map[string]any{"notification": map[string]any{"ID": "n1"}}},
	}
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", "2").Return(pr_db.PatronRequest{ID: patronRequestId, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("req-1")}, nil)
	mockPrRepo.On("GetNotificationById", "n1").Return(pr_db.Notification{ID: "n1", Note: pgtype.Text{String: "Say hi", Valid: true}}, nil)
	service := CreatePatronRequestNotificationService(mockPrRepo, new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, result)
}

func TestHandleInvokeNotificationMissingSymbol(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "2",
		EventData:       events.EventData{CustomData: map[string]any{"notification": map[string]any{"ID": "n1"}}},
	}
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", "2").Return(pr_db.PatronRequest{ID: patronRequestId, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: pgtype.Text{}, RequesterReqID: getDbText("req-1")}, nil)
	mockPrRepo.On("GetNotificationById", "n1").Return(pr_db.Notification{ID: "n1", Note: pgtype.Text{String: "Say hi", Valid: true}}, nil)
	service := CreatePatronRequestNotificationService(mockPrRepo, new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing requester symbol", result.EventError.Message)
}

func TestHandleInvokeNotificationHttpErrorResponse(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "error",
		EventData:       events.EventData{CustomData: map[string]any{"notification": map[string]any{"ID": "n1"}}},
	}
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", "error").Return(pr_db.PatronRequest{ID: "error", State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("error")}, nil)
	mockPrRepo.On("GetNotificationById", "n1").Return(pr_db.Notification{ID: "n1", Note: pgtype.Text{String: "Say hi", Valid: true}}, nil)
	service := CreatePatronRequestNotificationService(mockPrRepo, new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, "failed to send notification", result.EventError.Message)
}

func TestHandleInvokeNotificationHttpErrorResponseRequester(t *testing.T) {
	notificationEvent := events.Event{
		ID:              "event-1",
		PatronRequestID: "error",
		EventData:       events.EventData{CustomData: map[string]any{"notification": map[string]any{"ID": "n1"}}},
	}
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetPatronRequestById", "error").Return(pr_db.PatronRequest{ID: "error", State: BorrowerStateReceived, Side: SideBorrowing, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("error")}, nil)
	mockPrRepo.On("GetNotificationById", "n1").Return(pr_db.Notification{ID: "n1", Note: pgtype.Text{String: "Say hi", Valid: true}}, nil)
	service := CreatePatronRequestNotificationService(mockPrRepo, new(MockEventBus), new(MockIso18626Handler))

	status, result := service.handleInvokeNotification(appCtx, notificationEvent)
	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, "failed to send notification", result.EventError.Message)
}
