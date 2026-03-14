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
	"github.com/indexdata/crosslink/broker/ncipclient"
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
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateNew, Side: "helper", IllRequest: illRequest}, nil)

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

func TestHandleInvokeActionNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS creator not configured", resultData.EventError.Message)
}

func TestHandleBorrowingActionMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing requester symbol", resultData.EventError.Message)
}

func TestHandleInvokeActionValidateOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, mockEventBus, new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	fakeEventID := "1234"
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)
	mockEventBus.On("CreateNoticeWithParent", fakeEventID).Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{ID: fakeEventID, PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateValidated, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionValidateGetAdapterFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
	assert.Equal(t, "assert.AnError general error for testing", resultData.EventError.Cause)
}

func TestHandleInvokeActionValidateLookupFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS LookupUser failed", resultData.EventError.Message)
}

func TestHandleInvokeActionSendRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionSendRequest
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionReceiveOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}, {Barcode: "5678"}}, nil)

	action := BorrowerActionReceive
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateReceived, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionReceiveAcceptItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	action := BorrowerActionReceive
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS AcceptItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionReceiveNoItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)

	action := BorrowerActionReceive
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "receiveBorrowingRequest failed to get items by PR ID", resultData.EventError.Message)
	assert.Equal(t, "no items found for patron request", resultData.EventError.Cause)
}

func TestHandleInvokeActionReceiveItemLookupFailure(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)

	action := BorrowerActionReceive
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "receiveBorrowingRequest failed to get items by PR ID", resultData.EventError.Message)
	assert.Equal(t, "failed to get items: assert.AnError general error for testing", resultData.EventError.Cause)
}

func TestHandleInvokeActionCheckOutOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, Patron: pgtype.Text{Valid: true, String: "patron1"}, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedOut, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckOutFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckOutItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckInOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedIn, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckInFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckInItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionShipReturnOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateShippedReturned, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionShipReturnFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS DeleteItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionCancelRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateWillSupply, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionCancelRequest
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionAcceptCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionAcceptCondition
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateWillSupply, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionRejectCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
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
	var request iso18626.Request
	result := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}}, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid requester symbol", result.result.EventError.Message)
}

func TestSendBorrowingRequestZeroValueIllRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, nil)

	result := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateValidated,
		Side:            SideBorrowing,
		Patron:          pgtype.Text{Valid: true, String: "patron1"},
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"},
	}, iso18626.Request{})

	assert.Equal(t, events.EventStatusSuccess, result.status)
	if assert.NotNil(t, result.result) && assert.NotNil(t, result.result.OutgoingMessage) &&
		assert.NotNil(t, result.result.OutgoingMessage.Request) {
		request := result.result.OutgoingMessage.Request
		assert.Equal(t, "ISIL", request.Header.RequestingAgencyId.AgencyIdType.Text)
		assert.Equal(t, "REC1", request.Header.RequestingAgencyId.AgencyIdValue)
		assert.Equal(t, patronRequestId, request.Header.RequestingAgencyRequestId)
		assert.Equal(t, "patron1", request.PatronInfo.PatronId)
		assert.Equal(t, iso18626.BibliographicInfo{}, request.BibliographicInfo)
	}
	assert.Equal(t, iso18626.TypeMessageStatusOK, result.result.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
}

func TestShipReturnBorrowingRequestMissingSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	illRequest := iso18626.Request{}
	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateValidated, Side: SideBorrowing, Patron: pgtype.Text{Valid: true, String: "patron1"}, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "missing supplier symbol", result.result.EventError.Message)
}

func TestShipReturnBorrowingRequestMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateValidated, Side: SideBorrowing, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "missing requester symbol", result.result.EventError.Message)
}

func TestShipReturnBorrowingRequestInvalidSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "x"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid supplier symbol", result.result.EventError.Message)
}

func TestShipReturnBorrowingRequestInvalidRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "x"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid requester symbol", result.result.EventError.Message)
}

func TestHandleInvokeLenderActionNoSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateNew, Side: SideLending}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing supplier symbol", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), assert.AnError)

	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateNew, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionValidate(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, mockEventBus, mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}

	initialPR := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           LenderStateNew,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
	}
	validatedPR := initialPR
	validatedPR.State = LenderStateValidated
	willSupplyPR := validatedPR
	willSupplyPR.State = LenderStateWillSupply

	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPR, nil).Once()
	mockEventBus.On("CreateNoticeWithParent", "invoke-validate").Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-validate",
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.Len(t, mockEventBus.createdTaskData, 1)
	assert.NotNil(t, mockEventBus.createdTaskData[0].Action)
	assert.Equal(t, LenderActionWillSupply, *mockEventBus.createdTaskData[0].Action)
}

func TestHandleInvokeLenderActionWillSupplyOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionRejectCancel(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           LenderStateCancelRequested,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		RequesterReqID:  getDbText("req-1"),
	}, nil)
	action := LenderActionRejectCancel

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, iso18626.TypeStatusWillSupply, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo) {
		assert.Equal(t, iso18626.TypeYesNoN, *mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo)
	}
}

func TestHandleInvokeLenderActionWillSupplyNcipFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)

	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS RequestItem failed", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionWillSupplySaveItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.saveItemFail = true
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to save item", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionCannotSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionCannotSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateUnfilled, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionAddCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateConditionPending, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionShipOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}, {Barcode: "5678"}}, nil)

	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateShipped, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionShipGetItemsByIdFail(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)

	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "no items for shipping in the request", resultData.EventError.Message)
	assert.Equal(t, "failed to get items: assert.AnError general error for testing", resultData.EventError.Cause)
}

func TestHandleInvokeLenderActionShipGetItemsByIdEmpty(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)

	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "no items for shipping in the request", resultData.EventError.Message)
	assert.Equal(t, "no items found for patron request", resultData.EventError.Cause)
}

func TestHandleInvokeLenderActionShipLmsFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckOutItem failed", resultData.EventError.Message)
	assert.Equal(t, "CheckOutItem failed", resultData.EventError.Cause)
}

func TestHandleInvokeLenderActionMarkReceivedOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateShippedReturn, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	action := LenderActionMarkReceived
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateCompleted, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionMarkReceivedNoItems(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateShippedReturn, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	action := LenderActionMarkReceived
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "no items for check-in in the request", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionMarkReceivedLmsFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateShippedReturn, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	action := LenderActionMarkReceived
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckInItem failed", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionAcceptCancel(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateCancelRequested, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("req-1")}, nil)
	action := LenderActionAcceptCancel

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, LenderStateCancelled, mockPrRepo.savedPr.State)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, iso18626.TypeStatusCancelled, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo) {
		assert.Equal(t, iso18626.TypeYesNoY, *mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo)
	}
}

func TestHandleInvokeLenderActionAcceptCancelMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, *new(events.EventBus), mockIso18626Handler, lmsCreator)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateCancelRequested, Side: SideLending, RequesterSymbol: pgtype.Text{Valid: false, String: ""}, SupplierSymbol: getDbText("ISIL:SUP1")}, nil)
	action := LenderActionAcceptCancel

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing requester symbol", resultData.EventError.Message)
}

type MockEventBus struct {
	mock.Mock
	events.EventBus
	createdTaskData []events.EventData
	runTaskHandler  bool
}

func (m *MockEventBus) ProcessTask(ctx common.ExtendedContext, event events.Event, h func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	if m.runTaskHandler {
		status, result := h(ctx, event)
		event.EventStatus = status
		if result != nil {
			event.ResultData = *result
		}
		return event, nil
	}
	args := m.Called(event.ID)
	return args.Get(0).(events.Event), args.Error(1)
}

func (m *MockEventBus) CreateTask(id string, eventName events.EventName, data events.EventData, eventClass events.EventDomain, parentId *string) (string, error) {
	m.createdTaskData = append(m.createdTaskData, data)
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

func (m *MockEventBus) CreateNoticeWithParent(id string, eventName events.EventName, data events.EventData, status events.EventStatus, eventDomain events.EventDomain, parentId *string) (string, error) {
	if parentId == nil || id == "error" {
		return "", errors.New("event bus error")
	}
	args := m.Called(*parentId)
	return args.Get(0).(string), args.Error(1)
}

type MockPrRepo struct {
	mock.Mock
	pr_db.PgPrRepo
	savedPr            pr_db.PatronRequest
	savedItems         []pr_db.Item
	savedNotifications []pr_db.Notification
	saveItemFail       bool
}

func (r *MockPrRepo) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	args := r.Called(id)
	return args.Get(0).(pr_db.PatronRequest), args.Error(1)
}

func (r *MockPrRepo) GetPatronRequestByIdAndSide(ctx common.ExtendedContext, id string, side pr_db.PatronRequestSide) (pr_db.PatronRequest, error) {
	args := r.Called(id, side)
	return args.Get(0).(pr_db.PatronRequest), args.Error(1)
}

func (r *MockPrRepo) UpdatePatronRequest(ctx common.ExtendedContext, params pr_db.UpdatePatronRequestParams) (pr_db.PatronRequest, error) {
	if strings.Contains(params.ID, "error") || strings.Contains(params.RequesterReqID.String, "error") {
		return pr_db.PatronRequest{}, errors.New("db error")
	}
	r.savedPr = pr_db.PatronRequest(params)
	return pr_db.PatronRequest(params), nil
}

func (r *MockPrRepo) CreatePatronRequest(ctx common.ExtendedContext, params pr_db.CreatePatronRequestParams) (pr_db.PatronRequest, error) {
	if strings.Contains(params.ID, "error") || strings.Contains(params.RequesterReqID.String, "error") {
		return pr_db.PatronRequest{}, errors.New("db error")
	}
	r.savedPr = pr_db.PatronRequest(params)
	return pr_db.PatronRequest(params), nil
}

func (r *MockPrRepo) GetLendingRequestBySupplierSymbolAndRequesterReqId(ctx common.ExtendedContext, symbol string, requesterReqId string) (pr_db.PatronRequest, error) {
	args := r.Called(symbol, requesterReqId)
	return args.Get(0).(pr_db.PatronRequest), args.Error(1)
}

func (r *MockPrRepo) SaveItem(ctx common.ExtendedContext, params pr_db.SaveItemParams) (pr_db.Item, error) {
	if r.saveItemFail {
		return pr_db.Item{}, errors.New("db error")
	}
	for _, call := range r.ExpectedCalls {
		if call.Method == "SaveItem" {
			args := r.Called(params)
			if item, ok := args.Get(0).(pr_db.Item); ok {
				return item, args.Error(1)
			}
			return pr_db.Item{}, args.Error(1)
		}
	}

	if strings.Contains(params.PrID, "error") {
		return pr_db.Item{}, errors.New("db error")
	}
	if r.savedItems == nil {
		r.savedItems = []pr_db.Item{}
	}
	r.savedItems = append(r.savedItems, pr_db.Item(params))
	return pr_db.Item(params), nil
}

func (r *MockPrRepo) GetItemsByPrId(ctx common.ExtendedContext, id string) ([]pr_db.Item, error) {
	args := r.Called(id)
	return args.Get(0).([]pr_db.Item), args.Error(1)
}

func (r *MockPrRepo) SaveNotification(ctx common.ExtendedContext, params pr_db.SaveNotificationParams) (pr_db.Notification, error) {
	if r.savedNotifications == nil {
		r.savedNotifications = []pr_db.Notification{}
	}
	r.savedNotifications = append(r.savedNotifications, pr_db.Notification(params))
	if params.PrID == "error" {
		return pr_db.Notification{}, errors.New("db error")
	}
	return pr_db.Notification(params), nil
}

type MockIso18626Handler struct {
	mock.Mock
	handler.Iso18626Handler
	lastSupplyingAgencyMessage *iso18626.SupplyingAgencyMessage
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
	h.lastSupplyingAgencyMessage = illMessage.SupplyingAgencyMessage
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

func (m *MockLmsCreator) GetAdapter(ctx common.ExtendedContext, symbol string) (lms.LmsAdapter, error) {
	args := m.Called(symbol)
	return args.Get(0).(lms.LmsAdapter), args.Error(1)
}

func createLmsAdapterMockFail() lms.LmsAdapter {
	return &MockLmsAdapterFail{}
}

func createLmsAdapterMockLog() lms.LmsAdapter {
	return &MockLmsAdapterLog{}
}

type MockLmsAdapterLog struct {
	lms.LmsAdapter
	logFunc ncipclient.NcipLogFunc
}

func (l *MockLmsAdapterLog) SetLogFunc(logFunc ncipclient.NcipLogFunc) {
	l.logFunc = logFunc
}

func (l *MockLmsAdapterLog) LookupUser(patron string) (string, error) {
	if l.logFunc != nil {
		l.logFunc(map[string]any{"patron": patron}, map[string]any{"patron": patron}, nil)
	}
	return patron, nil
}

func (l *MockLmsAdapterLog) RequestItem(
	requestId string,
	itemId string,
	userId string,
	pickupLocation string,
	itemLocation string,
) (string, string, error) {
	return "", "", nil
}

func (l *MockLmsAdapterLog) InstitutionalPatron(requesterSymbol string) string {
	return ""
}

func (l *MockLmsAdapterLog) SupplierPickupLocation() string {
	return ""
}

func (l *MockLmsAdapterLog) ItemLocation() string {
	return ""
}

func (l *MockLmsAdapterLog) RequesterPickupLocation() string {
	return ""
}

type MockLmsAdapterFail struct {
}

func (l *MockLmsAdapterFail) SetLogFunc(logFunc ncipclient.NcipLogFunc) {
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
) (string, string, error) {
	return "", "", errors.New("RequestItem failed")
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
) (string, error) {
	return "", errors.New("CheckOutItem failed")
}

func (l *MockLmsAdapterFail) CreateUserFiscalTransaction(userId string, itemId string) error {
	return errors.New("CreateUserFiscalTransaction failed")
}

func (l *MockLmsAdapterFail) InstitutionalPatron(requesterSymbol string) string {
	return ""
}

func (l *MockLmsAdapterFail) SupplierPickupLocation() string {
	return ""
}

func (l *MockLmsAdapterFail) ItemLocation() string {
	return ""
}

func (*MockLmsAdapterFail) RequesterPickupLocation() string {
	return ""
}

func TestLoadReturnableStateModel(t *testing.T) {
	stateModel, err := LoadStateModelByName("returnables")
	assert.Nil(t, err)
	assert.NotNil(t, stateModel)
}
