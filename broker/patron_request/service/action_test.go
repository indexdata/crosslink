package prservice

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var appCtx = common.CreateExtCtxWithArgs(context.Background(), nil)
var patronRequestId = "pr1"

var actionValidate = BorrowerActionValidate

func TestInvokeAction(t *testing.T) {
	mockEventBus := new(MockEventBus)
	prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), mockEventBus, new(handler.Iso18626Handler), nil)
	event := events.Event{
		ID: "action-1",
	}
	mockEventBus.On("ProcessTask", event.ID).Return(event, nil)

	prAction.InvokeAction(appCtx, event)

	mockEventBus.AssertNumberOfCalls(t, "ProcessTask", 1)
}

func TestHandleInvokeActionNotSpecifiedAction(t *testing.T) {
	prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), *new(events.EventBus), new(handler.Iso18626Handler), nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "action not specified", resultData.EventError.Message)
}

func TestHandleInvokeActionNoPR(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, errors.New("not fund"))

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to read patron request", resultData.EventError.Message)
}

func TestHandleInvokeActionNoPRSide(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateNew, Side: "helper"}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "side helper is not supported", resultData.EventError.Message)
}

func TestHandleInvokeActionWhichIsNotAllowed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "state VALIDATED does not support action validate", resultData.EventError.Message)
}

func TestHandleInvoceActionNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS creator not configured", resultData.EventError.Message)
}

func TestHandleInvokeActionValidateOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:x"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateValidated, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionValidateGetAdapterFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:x"}).Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
	assert.Equal(t, "assert.AnError general error for testing", resultData.EventError.Cause)
}

func TestHandleInvokeActionValidateLookupFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS LookupUser failed", resultData.EventError.Message)
}

func TestHandleInvokeActionSendRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionSendRequest
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionSendRequestError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionSendRequest

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to parse request", resultData.EventError.Message)
}

func TestHandleInvokeActionReceiveOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionReceive
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateReceived, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionReceiveBadIllRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := []byte("{\"bad\": {}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionReceive
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to unmarshal ILL request", resultData.EventError.Message)
}

func TestHandleInvokeActionReceiveGetAdapterFail(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	action := BorrowerActionReceive
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
}

func TestHandleInvokeActionReceiveAcceptItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	action := BorrowerActionReceive
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS AcceptItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckOutOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, Patron: pgtype.Text{Valid: true, String: "patron1"}, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedOut, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckOutGetAdapterFail(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	action := BorrowerActionCheckOut
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckOutBadIllRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"bad\": {}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to unmarshal ILL request", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckOutFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckOutItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckInOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedIn, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckInGetAdapterFail(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckInBadIllRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"bad\": {}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to unmarshal ILL request", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckInFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckInItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionShipReturnOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateShippedReturned, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionShipReturnFailCreator(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
}

func TestHandleInvokeActionShipReturnFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := []byte("{\"request\": {}}")
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS DeleteItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionCancelRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateWillSupply, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionCancelRequest
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionAcceptCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionAcceptCondition
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateWillSupply, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionRejectCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionRejectCondition
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestSendBorrowingRequestInvalidSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, nil)
	status, resultData := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "invalid requester symbol", resultData.EventError.Message)
}

func TestShipReturnBorrowingRequestMissingSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)

	illRequest := []byte("{\"request\": {}}")
	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateValidated, Side: SideBorrowing, Patron: pgtype.Text{Valid: true, String: "patron1"}, ID: "1", RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, lmsAdapter)

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing supplier symbol", resultData.EventError.Message)
}

func TestShipReturnBorrowingRequestBadIllRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)

	illRequest := []byte("{bad")
	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{IllRequest: illRequest, Patron: pgtype.Text{Valid: true, String: "patron1"}, ID: "1", State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, lmsAdapter)

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to unmarshal ILL request", resultData.EventError.Message)
}

func TestShipReturnBorrowingRequestMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)

	illRequest := []byte("{\"request\": {}}")
	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{IllRequest: illRequest, ID: "1", State: BorrowerStateValidated, Side: SideBorrowing, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, lmsAdapter)

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing requester symbol", resultData.EventError.Message)
}

func TestShipReturnBorrowingRequestInvalidSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)

	illRequest := []byte("{\"request\": {}}")
	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{IllRequest: illRequest, ID: "1", State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "x"}}, lmsAdapter)

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "invalid supplier symbol", resultData.EventError.Message)
}

func TestShipReturnBorrowingRequestInvalidRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "x"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)

	illRequest := []byte("{\"request\": {}}")
	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{IllRequest: illRequest, ID: "1", State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, lmsAdapter)

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "invalid requester symbol", resultData.EventError.Message)
}

func TestHandleInvokeLenderLmsCreatorNotConfigured(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateNew, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS creator not configured", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionValidate(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)

	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateNew, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionWillSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionCannotSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionCannotSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateUnfilled, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionAddCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateConditionPending, mockPrRepo.savedPr.State)
}
func TestHandleInvokeLenderActionShip(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateShipped, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionMarkReceived(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateShippedReturn, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionMarkReceived
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateCompleted, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionMarkCancelled(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateCancelRequested, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionMarkCancelled

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateCancelled, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionMarkCancelledMissingSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateCancelRequested, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: false, String: ""}, RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionMarkCancelled

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing supplier symbol", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionMarkCancelledMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:SUP1"}).Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: LenderStateCancelRequested, Side: SideLending, RequesterSymbol: pgtype.Text{Valid: false, String: ""}, SupplierSymbol: getDbText("ISIL:SUP1")}, nil)
	action := LenderActionMarkCancelled

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing requester symbol", resultData.EventError.Message)
}

type MockEventBus struct {
	mock.Mock
	events.EventBus
}

func (m *MockEventBus) ProcessTask(ctx common.ExtendedContext, event events.Event, h func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	args := m.Called(event.ID)
	return args.Get(0).(events.Event), args.Error(1)
}

func (m *MockEventBus) CreateTask(id string, eventName events.EventName, data events.EventData, eventClass events.EventDomain, parentId *string) (string, error) {
	if id == "error" {
		return "", errors.New("event bus error")
	}
	return id, nil
}

func (m *MockEventBus) CreateNotice(id string, eventName events.EventName, data events.EventData, status events.EventStatus, eventDomain events.EventDomain) (string, error) {
	if id == "error" {
		return "", errors.New("event bus error")
	}
	return id, nil
}

type MockPrRepo struct {
	mock.Mock
	pr_db.PgPrRepo
	savedPr pr_db.PatronRequest
}

func (r *MockPrRepo) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	args := r.Called(id)
	return args.Get(0).(pr_db.PatronRequest), args.Error(1)
}

func (r *MockPrRepo) SavePatronRequest(ctx common.ExtendedContext, params pr_db.SavePatronRequestParams) (pr_db.PatronRequest, error) {
	if strings.Contains(params.ID, "error") || strings.Contains(params.RequesterReqID.String, "error") {
		return pr_db.PatronRequest{}, errors.New("db error")
	}
	r.savedPr = pr_db.PatronRequest(params)
	return pr_db.PatronRequest(params), nil
}

func (r *MockPrRepo) GetPatronRequestBySupplierSymbolAndRequesterReqId(ctx common.ExtendedContext, symbol string, requesterReqId string) (pr_db.PatronRequest, error) {
	args := r.Called(symbol, requesterReqId)
	return args.Get(0).(pr_db.PatronRequest), args.Error(1)
}

type MockIso18626Handler struct {
	mock.Mock
	handler.Iso18626Handler
}

func (h *MockIso18626Handler) HandleRequest(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	status := iso18626.TypeMessageStatusOK
	if illMessage.Request.Header.RequestingAgencyRequestId == "error" {
		status = iso18626.TypeMessageStatusERROR
	}
	var resmsg = &iso18626.ISO18626Message{
		RequestConfirmation: &iso18626.RequestConfirmation{
			ConfirmationHeader: iso18626.ConfirmationHeader{
				MessageStatus: status,
			},
		},
	}
	output, err := xml.MarshalIndent(resmsg, "  ", "  ")
	if err != nil {
		ctx.Logger().Error("failed to produce response", "error", err, "body", string(output))
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}

func (h *MockIso18626Handler) HandleRequestingAgencyMessage(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	status := iso18626.TypeMessageStatusOK
	if illMessage.RequestingAgencyMessage.Header.RequestingAgencyRequestId == "error" {
		status = iso18626.TypeMessageStatusERROR
	}
	var resmsg = &iso18626.ISO18626Message{
		RequestingAgencyMessageConfirmation: &iso18626.RequestingAgencyMessageConfirmation{
			ConfirmationHeader: iso18626.ConfirmationHeader{
				MessageStatus: status,
			},
		},
	}
	output, err := xml.MarshalIndent(resmsg, "  ", "  ")
	if err != nil {
		ctx.Logger().Error("failed to produce response", "error", err, "body", string(output))
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}
func (h *MockIso18626Handler) HandleSupplyingAgencyMessage(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	status := iso18626.TypeMessageStatusOK
	if illMessage.SupplyingAgencyMessage.Header.RequestingAgencyRequestId == "error" {
		status = iso18626.TypeMessageStatusERROR
	}
	var resmsg = &iso18626.ISO18626Message{
		SupplyingAgencyMessageConfirmation: &iso18626.SupplyingAgencyMessageConfirmation{
			ConfirmationHeader: iso18626.ConfirmationHeader{
				MessageStatus: status,
			},
		},
	}
	output, err := xml.MarshalIndent(resmsg, "  ", "  ")
	if err != nil {
		ctx.Logger().Error("failed to produce response", "error", err, "body", string(output))
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}

type MockLmsCreator struct {
	mock.Mock
	lms.LmsCreator
}

func (m *MockLmsCreator) GetAdapter(ctx common.ExtendedContext, symbol pgtype.Text) (lms.LmsAdapter, error) {
	args := m.Called(symbol)
	return args.Get(0).(lms.LmsAdapter), args.Error(1)
}

func createLmsAdapterMockFail() lms.LmsAdapter {
	return &MockLmsAdapterFail{}
}

type MockLmsAdapterFail struct {
}

func (l *MockLmsAdapterFail) LookupUser(patron string) (string, error) {
	return "", errors.New("LookupUser failed")
}

func (l *MockLmsAdapterFail) AcceptItem(
	itemId string,
	requestId string,
	userId string,
	author string,
	title string,
	isbn string,
	callNumber string,
	pickupLocation string,
	requestedAction string,
) error {
	return errors.New("AcceptItem failed")
}

func (l *MockLmsAdapterFail) DeleteItem(itemId string) error {
	return errors.New("DeleteItem failed")
}

func (l *MockLmsAdapterFail) RequestItem(
	requestId string,
	itemId string,
	borrowerBarcode string,
	pickupLocation string,
	itemLocation string,
) error {
	return errors.New("RequestItem failed")
}

func (l *MockLmsAdapterFail) CancelRequestItem(requestId string, userId string) error {
	return errors.New("CancelRequestItem failed")
}

func (l *MockLmsAdapterFail) CheckInItem(itemId string) error {
	return errors.New("CheckInItem failed")
}

func (l *MockLmsAdapterFail) CheckOutItem(
	requestId string,
	itemBarcode string,
	borrowerBarcode string,
	externalReferenceValue string,
) error {
	return errors.New("CheckOutItem failed")
}

func (l *MockLmsAdapterFail) CreateUserFiscalTransaction(userId string, itemId string) error {
	return errors.New("CreateUserFiscalTransaction failed")
}

func (l *MockLmsAdapterFail) InstitutionalPatron() string {
	return ""
}

func TestPickupLocationFromIllRequest(t *testing.T) {
	var r iso18626.Request
	r.RequestedDeliveryInfo = []iso18626.RequestedDeliveryInfo{{
		Address: &iso18626.Address{
			PhysicalAddress: &iso18626.PhysicalAddress{
				Line1:      "Main Library",
				Line2:      "123 Library St",
				Locality:   "Booktown",
				PostalCode: "12345",
				Region:     &iso18626.TypeSchemeValuePair{Text: "State"},
				Country:    &iso18626.TypeSchemeValuePair{Text: "Country"},
			},
		},
	}}
	location := pickupLocationFromIllRequest(r)
	assert.Equal(t, "Main Library 123 Library St Booktown 12345 State Country", location)
}

func TestIsbnFromIllRequest(t *testing.T) {
	var r iso18626.Request
	r.BibliographicInfo = iso18626.BibliographicInfo{
		BibliographicItemId: []iso18626.BibliographicItemId{
			{
				BibliographicItemIdentifier: "978-3-16-148410-0",
				BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{
					Text: "ISBN",
				},
			},
		},
	}
	isbn := isbnFromIllRequest(r)
	assert.Equal(t, "978-3-16-148410-0", isbn)
}

func TestCallNumberFromIllRequest(t *testing.T) {
	var r iso18626.Request
	r.SupplierInfo = []iso18626.SupplierInfo{{
		CallNumber: "QA76.73.G63 D37 2020",
	}}
	callNumber := callNumberFromIllRequest(r)
	assert.Equal(t, "QA76.73.G63 D37 2020", callNumber)
}
