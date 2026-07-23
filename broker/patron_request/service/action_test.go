package prservice

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/lms"
	"github.com/indexdata/crosslink/broker/ncipclient"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/indexdata/crosslink/broker/shim"
	"github.com/indexdata/crosslink/directory"
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
	prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), new(IllRepoMock), mockEventBus, new(handler.Iso18626Handler), nil, new(EmailSenderMock))
	event := events.Event{
		ID: "action-1",
	}
	mockEventBus.On("ProcessExclusiveTask", event.ID).Return(event, nil)

	prAction.InvokeAction(appCtx, event)

	mockEventBus.AssertNumberOfCalls(t, "ProcessExclusiveTask", 1)
}

func TestHandleInvokeActionNotSpecifiedAction(t *testing.T) {
	prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock))

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "action not specified", resultData.EventError.Message)
}

func TestHandleInvokeActionNoPR(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock))
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, errors.New("not fund"))

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to read patron request", resultData.EventError.Message)
}

func TestHandleInvokeActionNoPRSide(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateNew, Side: "helper", IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "state NEW does not support action validate", resultData.EventError.Message)
}

func TestHandleInvokeActionWhichIsNotAllowed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock))
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "state VALIDATED does not support action validate", resultData.EventError.Message)
}

func TestHandleInvokeActionNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS creator not configured", resultData.EventError.Message)
}

func TestHandleInvokeActionTerminateOKNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock))
	action := TerminateAction
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:            patronRequestId,
		State:         BorrowerStateValidated,
		Side:          SideBorrowing,
		TerminalState: false,
	}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.NotNil(t, resultData.ActionResult)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, string(BorrowerStateManuallyClosed), *resultData.ActionResult.ToState)
	assert.Equal(t, BorrowerStateManuallyClosed, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.Equal(t, string(TerminateAction), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeSuccess, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(events.EventStatusSuccess), mockPrRepo.savedPr.LastActionResult.String)
}

func TestHandleInvokeActionTerminateRejectsTerminal(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock))
	action := TerminateAction
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:            patronRequestId,
		State:         BorrowerStateCompleted,
		Side:          SideBorrowing,
		TerminalState: true,
	}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "patron request "+patronRequestId+" is already terminal", resultData.EventError.Message)
}

func TestHandleBorrowingActionMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing requester symbol", resultData.EventError.Message)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeActionValidateNeedReview(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	fakeEventID := "1234"
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)
	mockEventBus.On("CreateNoticeWithParent", fakeEventID).Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{ID: fakeEventID, PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateNeedsReview, mockPrRepo.savedPr.State)
	assert.Equal(t, ActionOutcomeReview, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(BorrowerStateNeedsReview), *resultData.ActionResult.ToState)
}

func TestHandleInvokeActionValidateSendRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)
	mockIso18626Handler := new(MockIso18626Handler)

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "12312"}}
	fakeEventID := "1234"
	initialPR := pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}
	validatedPR := initialPR
	validatedPR.State = BorrowerStateValidated
	sentPR := initialPR
	sentPR.State = BorrowerStateSent
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(sentPR, nil).Once()
	mockEventBus.On("CreateNoticeWithParent", fakeEventID).Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{ID: fakeEventID, PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
	assert.Equal(t, string(BorrowerStateValidated), *resultData.ActionResult.ToState)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
}

func TestHandleInvokeActionValidateSendRequestDuplicate(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)
	mockIso18626Handler := new(MockIso18626Handler)
	patronRequestId := "duplicate"

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "12312"}}
	fakeEventID := "1234"
	initialPR := pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}
	validatedPR := initialPR
	validatedPR.State = BorrowerStateValidated
	sentPR := initialPR
	sentPR.State = BorrowerStateSent
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(sentPR, nil).Once()
	mockEventBus.On("CreateNoticeWithParent", fakeEventID).Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{ID: fakeEventID, PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateClosedDuplicate, mockPrRepo.savedPr.State)
	assert.Equal(t, string(BorrowerActionSendRequest), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeDuplicate, mockPrRepo.savedPr.LastActionOutcome.String)
}

func TestHandleInvokeActionValidateGetAdapterFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest, NeedsAttention: true}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
	assert.Equal(t, "assert.AnError general error for testing", resultData.EventError.Cause)
}

func TestHandleInvokeActionValidateLookupFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS LookupUser failed", resultData.EventError.Message)
}

func TestHandleInvokeActionSendRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("AcceptItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
		{
			ID:        "item1",
			PrID:      patronRequestId,
			Barcode:   "1234",
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		},
		{
			ID:        "item2",
			PrID:      patronRequestId,
			Barcode:   "5678",
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		},
	}, nil)

	action := BorrowerActionReceive
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateReceived, mockPrRepo.savedPr.State)
	lmsAdapter.AssertNumberOfCalls(t, "AcceptItem", 2)
	if assert.Len(t, lmsAdapter.Calls, 2) {
		assert.Equal(t, "AcceptItem", lmsAdapter.Calls[0].Method)
		assert.Equal(t, "1234", lmsAdapter.Calls[0].Arguments.String(0))
		assert.Equal(t, "AcceptItem", lmsAdapter.Calls[1].Method)
		assert.Equal(t, "5678", lmsAdapter.Calls[1].Arguments.String(0))
	}
}

func TestHandleInvokeActionReceiveAcceptItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	action := BorrowerActionReceive
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS AcceptItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionReceiveNoItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, Patron: pgtype.Text{Valid: true, String: "patron1"}, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedOut, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckOutItemFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, Patron: pgtype.Text{Valid: true, String: "patron1"}, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "checkoutBorrowingRequest failed to get items by PR ID", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckOutFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	action := BorrowerActionCheckOut
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})
	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckOutItem failed", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckInOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedIn, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckInItemFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	action := BorrowerActionCheckIn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "checkinBorrowingRequest failed to get items by PR ID", resultData.EventError.Message)
}

func TestHandleInvokeActionCheckInFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedOut, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateShippedReturned, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionShipReturnItemFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "shipReturnBorrowingRequest failed to get items by PR ID", resultData.EventError.Message)
}

func TestHandleInvokeActionShipReturnFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionAcceptCondition
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData) && assert.NotNil(t, resultData.IncomingMessage) {
		assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	}
	if assert.NotNil(t, mockIso18626Handler.lastRequestingAgencyMessage) {
		assert.Equal(t, iso18626.TypeActionNotification, mockIso18626Handler.lastRequestingAgencyMessage.Action)
		assert.Equal(t, shim.RESHARE_LOAN_CONDITION_AGREE, mockIso18626Handler.lastRequestingAgencyMessage.Note)
		assert.False(t, mockIso18626Handler.lastRequestingAgencyMessage.Header.Timestamp.IsZero())
	}
	assert.Equal(t, BorrowerStateWillSupply, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionAcceptConditionMarksReceivedConditionNotificationsAccepted(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}, NeedsAttention: true}, nil)
	action := BorrowerActionAcceptCondition

	status, _ := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.Len(t, mockPrRepo.markedConditionNotificationsReceipts, 1) {
		assert.Equal(t, pr_db.MarkConditionNotificationsReceiptParams{
			Receipt:   string(pr_db.NotificationAccepted),
			PrID:      patronRequestId,
			Direction: string(pr_db.NotificationDirectionReceived),
		}, mockPrRepo.markedConditionNotificationsReceipts[0])
	}
	// Successful action resets NeedsAttention flag
	assert.False(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeActionRejectCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionRejectCondition
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData) && assert.NotNil(t, resultData.IncomingMessage) {
		assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	}
	if assert.NotNil(t, mockIso18626Handler.lastRequestingAgencyMessage) {
		assert.Equal(t, iso18626.TypeActionCancel, mockIso18626Handler.lastRequestingAgencyMessage.Action)
		assert.Equal(t, shim.RESHARE_LOAN_CONDITION_REJECT, mockIso18626Handler.lastRequestingAgencyMessage.Note)
		assert.False(t, mockIso18626Handler.lastRequestingAgencyMessage.Header.Timestamp.IsZero())
	}
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionRejectConditionMarksReceivedConditionNotificationsRejected(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionRejectCondition

	status, _ := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.Len(t, mockPrRepo.markedConditionNotificationsReceipts, 1) {
		assert.Equal(t, pr_db.MarkConditionNotificationsReceiptParams{
			Receipt:   string(pr_db.NotificationRejected),
			PrID:      patronRequestId,
			Direction: string(pr_db.NotificationDirectionReceived),
		}, mockPrRepo.markedConditionNotificationsReceipts[0])
	}
}

func TestSendBorrowingRequestInvalidSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, nil, new(EmailSenderMock))
	var request iso18626.Request
	result := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}}, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid requester symbol", result.result.EventError.Message)
}

func TestSendBorrowingRequestZeroValueIllRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, nil, new(EmailSenderMock))

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
		assert.False(t, request.Header.Timestamp.IsZero())
		assert.Equal(t, "patron1", request.PatronInfo.PatronId)
		assert.Equal(t, iso18626.BibliographicInfo{}, request.BibliographicInfo)
	}
	assert.Equal(t, iso18626.TypeMessageStatusOK, result.result.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
}

func TestSendBorrowingRequestPreservesIllRequestFields(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, nil, new(EmailSenderMock))

	requestType := iso18626.TypeRequestTypeNew
	illRequest := iso18626.Request{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType:  iso18626.TypeSchemeValuePair{Text: "OLD"},
				AgencyIdValue: "OLD_REQ",
			},
			RequestingAgencyRequestId: "old-id",
		},
		BibliographicInfo: iso18626.BibliographicInfo{
			Title: "preserved-title",
		},
		ServiceInfo: &iso18626.ServiceInfo{
			ServiceType: iso18626.TypeServiceTypeCopy,
			RequestType: &requestType,
			Note:        "preserve me",
		},
		RequestedDeliveryInfo: []iso18626.RequestedDeliveryInfo{
			{SortOrder: 1},
		},
		PatronInfo: &iso18626.PatronInfo{
			PatronId: "old-patron",
			Surname:  "Doe",
		},
	}

	result := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateValidated,
		Side:            SideBorrowing,
		Patron:          pgtype.Text{Valid: true, String: "patron1"},
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"},
	}, illRequest)

	assert.Equal(t, events.EventStatusSuccess, result.status)
	if assert.NotNil(t, result.result) && assert.NotNil(t, result.result.OutgoingMessage) &&
		assert.NotNil(t, result.result.OutgoingMessage.Request) {
		request := result.result.OutgoingMessage.Request
		assert.Equal(t, "ISIL", request.Header.RequestingAgencyId.AgencyIdType.Text)
		assert.Equal(t, "REC1", request.Header.RequestingAgencyId.AgencyIdValue)
		assert.Equal(t, patronRequestId, request.Header.RequestingAgencyRequestId)
		if assert.NotNil(t, request.PatronInfo) {
			assert.Equal(t, "patron1", request.PatronInfo.PatronId)
			assert.Equal(t, "Doe", request.PatronInfo.Surname)
		}
		if assert.NotNil(t, request.ServiceInfo) {
			assert.Equal(t, "preserve me", request.ServiceInfo.Note)
			assert.Equal(t, iso18626.TypeServiceTypeCopy, request.ServiceInfo.ServiceType)
		}
		assert.Equal(t, "preserved-title", request.BibliographicInfo.Title)
		assert.Len(t, request.RequestedDeliveryInfo, 1)
		assert.Equal(t, int64(1), request.RequestedDeliveryInfo[0].SortOrder)
	}
	assert.Equal(t, "OLD", illRequest.Header.RequestingAgencyId.AgencyIdType.Text)
	assert.Equal(t, "OLD_REQ", illRequest.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "old-id", illRequest.Header.RequestingAgencyRequestId)
	if assert.NotNil(t, illRequest.PatronInfo) {
		assert.Equal(t, "old-patron", illRequest.PatronInfo.PatronId)
		assert.Equal(t, "Doe", illRequest.PatronInfo.Surname)
	}
	assert.Equal(t, iso18626.TypeMessageStatusOK, result.result.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
}

func TestShipReturnBorrowingRequestMissingSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid requester symbol", result.result.EventError.Message)
}

func TestHandleInvokeLenderActionNoSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateNew, Side: SideLending}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing supplier symbol", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), assert.AnError)

	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateNew, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate, User: "okapi-user-1"}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.Len(t, mockEventBus.createdTaskData, 1)
	assert.NotNil(t, mockEventBus.createdTaskData[0].Action)
	assert.Equal(t, LenderActionWillSupply, *mockEventBus.createdTaskData[0].Action)
	assert.Equal(t, "okapi-user-1", mockEventBus.createdTaskData[0].User)
}

func TestHandleInvokeLenderActionValidateAutoActionError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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

	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, errors.New("db error")).Once()
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(validatedPR, nil).Once()
	mockEventBus.On("CreateNoticeWithParent", "invoke-validate").Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-validate",
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Equal(t, "auto action will-supply failed with status ERROR: failed to read patron request", *resultData.ActionResult.ChildActionError)
	}
	assert.True(t, mockPrRepo.savedPr.LastAction.Valid)
	assert.Equal(t, string(LenderActionWillSupply), mockPrRepo.savedPr.LastAction.String)
	assert.True(t, mockPrRepo.savedPr.LastActionOutcome.Valid)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.True(t, mockPrRepo.savedPr.LastActionResult.Valid)
	assert.Equal(t, string(events.EventStatusError), mockPrRepo.savedPr.LastActionResult.String)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeLenderActionValidateAutoActionCreateTaskError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.createTaskErr = errors.New("event bus error")
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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

	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(validatedPR, nil).Once()
	mockEventBus.On("CreateNoticeWithParent", "invoke-validate").Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-validate",
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidate}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Equal(t, "event bus error", *resultData.ActionResult.ChildActionError)
	}
	assert.True(t, mockPrRepo.savedPr.LastAction.Valid)
	assert.Equal(t, string(LenderActionWillSupply), mockPrRepo.savedPr.LastAction.String)
	assert.True(t, mockPrRepo.savedPr.LastActionOutcome.Valid)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.True(t, mockPrRepo.savedPr.LastActionResult.Valid)
	assert.Equal(t, string(events.EventStatusError), mockPrRepo.savedPr.LastActionResult.String)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeLenderActionWillSupplyUseIllTitleWhenRequestItemEmptyOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	lmsAdapter.On("RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("1", "2", "", nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{Title: "title1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.Len(t, mockPrRepo.savedItems, 1)
	assert.Equal(t, "1", mockPrRepo.savedItems[0].Barcode)
	assert.Equal(t, "2", mockPrRepo.savedItems[0].CallNumber.String)
	assert.Equal(t, "title1", mockPrRepo.savedItems[0].Title.String)

	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusWillSupply, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo)
	}
}

func TestHandleInvokeLenderActionWillSupplyUseRequestItemTitleWhenAvailableOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	lmsAdapter.On("RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("1", "2", "title2", nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{Title: "title1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note": "my note",
		},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.Len(t, mockPrRepo.savedItems, 1)
	assert.Equal(t, "1", mockPrRepo.savedItems[0].Barcode)
	assert.Equal(t, "2", mockPrRepo.savedItems[0].CallNumber.String)
	assert.Equal(t, "title2", mockPrRepo.savedItems[0].Title.String)

	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusWillSupply, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "my note", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo)
	}
}

func TestHandleInvokeLenderActionRejectCancel(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
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
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, iso18626.TypeStatusWillSupply, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
	assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.Header.Timestamp.IsZero())
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo) {
		assert.Equal(t, iso18626.TypeYesNoN, *mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo)
	}
}

func TestHandleInvokeLenderActionWillSupplyNcipFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))

	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS RequestItem failed", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionWillSupplySaveItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", "req-1", "", "", "", "").Return("item-1", "", "", nil)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.saveItemFail = true
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	action := LenderActionWillSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to save item", resultData.EventError.Message)
	lmsAdapter.AssertExpectations(t)
}

func TestCancelLenderRequestItemNoSavedItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	lmsAdapter := new(mockLmsAdapter)

	err := prAction.cancelLenderRequestItem(
		appCtx,
		pr_db.PatronRequest{ID: patronRequestId},
		lmsAdapter,
		iso18626.Request{},
	)

	assert.NoError(t, err)
	lmsAdapter.AssertNotCalled(t, "CancelRequestItem", mock.Anything, mock.Anything)
}

func TestCancelLenderRequestItemMissingRequestID(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	lmsAdapter := new(mockLmsAdapter)

	err := prAction.cancelLenderRequestItem(
		appCtx,
		pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: getDbText("ISIL:REQ1")},
		lmsAdapter,
		iso18626.Request{},
	)

	assert.EqualError(t, err, "missing RequestingAgencyRequestId for LMS CancelRequestItem")
	lmsAdapter.AssertNotCalled(t, "CancelRequestItem", mock.Anything, mock.Anything)
}

func TestCancelLenderRequestItemInvalidRequesterSymbol(t *testing.T) {
	tests := []struct {
		name   string
		symbol pgtype.Text
	}{
		{name: "not valid"},
		{name: "empty", symbol: getDbText("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
			prAction := &PatronRequestActionService{prRepo: mockPrRepo}
			lmsAdapter := new(mockLmsAdapter)

			err := prAction.cancelLenderRequestItem(
				appCtx,
				pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: tt.symbol},
				lmsAdapter,
				iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}},
			)

			assert.EqualError(t, err, "invalid requester symbol")
			lmsAdapter.AssertNotCalled(t, "CancelRequestItem", mock.Anything, mock.Anything)
		})
	}
}

func TestHandleInvokeLenderActionCannotSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	action := LenderActionCannotSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateUnfilled, mockPrRepo.savedPr.State)
	lmsAdapter.AssertExpectations(t)

	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusUnfilled, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonUnfilled)
	}
}

func TestHandleInvokeLenderActionCannotSupplyWithReason(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	action := LenderActionCannotSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note":           "my note",
			"reasonUnfilled": "my reason",
		},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateUnfilled, mockPrRepo.savedPr.State)

	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusUnfilled, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "my note", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo)
		if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonUnfilled) {
			assert.Equal(t, "my reason", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonUnfilled.Text)
		}
	}
}

func TestHandleInvokeLenderActionCannotSupplyCancelRequestItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(errors.New("cancel failed"))
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	action := LenderActionCannotSupply

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
	}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CancelRequestItem failed", resultData.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionAddConditionOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"loanCondition": "my condition",
		},
	}})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateConditionPending, mockPrRepo.savedPr.State)

	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusWillSupply, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "#ReShareAddLoanCondition#", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts)
		if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo) {
			assert.Equal(t, "my condition", mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo.LoanCondition.Text)
		}
	}
	if assert.Len(t, mockPrRepo.savedNotifications, 1) {
		n := mockPrRepo.savedNotifications[0]
		assert.Equal(t, pr_db.NotificationDirectionSent, n.Direction)
		assert.Equal(t, pr_db.NotificationKindCondition, n.Kind)
		assert.Equal(t, "ISIL:SUP1", n.FromSymbol)
		assert.Equal(t, "ISIL:REQ1", n.ToSymbol)
		assert.False(t, n.Note.Valid)
		assert.Equal(t, "my condition", n.Condition.String)
	}
}

func TestHandleInvokeLenderActionAskRetryMissingItemId(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAskRetry

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note":        "isbn",
			"reasonRetry": string(iso18626.ReasonRetryNotFoundAsCited),
		},
	}})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, "missing itemId for ask-retry action when reasonRetry is NotFoundAsCited", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionAskRetryCost(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAskRetry

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note":        "too low",
			"reasonRetry": string(iso18626.ReasonRetryCostExceedsMaxCost),
		},
	}})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, "unsupported reasonRetry \"CostExceedsMaxCost\" for ask-retry action (supported: \"NotFoundAsCited\")", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionAskRetryMissingReasonRetry(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAskRetry

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note":   "isbn",
			"itemId": "0201896834",
		},
	}})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, "missing reasonRetry for ask-retry action", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionAskRetryFull(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	action := LenderActionAskRetry

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note":        "isbn",
			"itemId":      "0201896834",
			"reasonRetry": string(iso18626.ReasonRetryNotFoundAsCited),
		},
	}})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateCompletedWithRetry, mockPrRepo.savedPr.State)
	lmsAdapter.AssertExpectations(t)

	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusRetryPossible, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, string(iso18626.ReasonRetryNotFoundAsCited), mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonRetry.Text)
		assert.Equal(t, "isbn", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts)
		if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo) {
			assert.Equal(t, "0201896834", mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo.ItemId)
		}
	}
}

func TestHandleInvokeLenderActionAskRetryCancelRequestItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(errors.New("cancel failed"))
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	action := LenderActionAskRetry

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"itemId":      "0201896834",
			"reasonRetry": string(iso18626.ReasonRetryNotFoundAsCited),
		},
	}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CancelRequestItem failed", resultData.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.False(t, mockPrRepo.savedPr.TerminalState)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeBorrowerActionAcceptRetryAutoActionCreateTaskError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.createTaskErr = errors.New("event bus error")
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(MockIllRepo), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	initialPR := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           BorrowerStateRetryPending,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REQ1"),
		SupplierSymbol:  getDbText("ISIL:SUP1"),
	}

	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(initialPR, nil).Once()

	action := BorrowerActionAcceptRetry
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-accept-retry",
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Equal(t, "event bus error", *resultData.ActionResult.ChildActionError)
	}
	// The original PR (not the retry PR) should be marked as a chain failure.
	assert.Equal(t, patronRequestId, mockPrRepo.savedPr.ID)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, string(BorrowerActionValidate), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(events.EventStatusError), mockPrRepo.savedPr.LastActionResult.String)
	assert.Equal(t, "REQ1-2", mockPrRepo.createdPr.ID)
	assert.Equal(t, "REQ1-2", mockPrRepo.createdPr.RequesterReqID.String)
	assert.Equal(t, "REQ1-2", mockPrRepo.createdPr.IllRequest.Header.RequestingAgencyRequestId)
}

func TestHandleInvokeLenderActionAddConditionMissingConditionAndCost(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note": "Condition note",
		},
	}})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.Equal(t, "loanCondition or cost is required", resultData.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Len(t, mockPrRepo.savedNotifications, 0)
}

func TestHandleInvokeLenderActionAddConditionWithCurrency(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"loanCondition": "my condition",
			"note":          "Condition note",
			"cost":          12.34,
			"currency":      "DKK",
		},
	}})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateConditionPending, mockPrRepo.savedPr.State)

	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusWillSupply, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "Condition note\n#ReShareAddLoanCondition#", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts) {
			assert.Equal(t, 1234, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Base)
			assert.Equal(t, 2, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts.MonetaryValue.Exp)
			assert.Equal(t, "DKK", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.OfferedCosts.CurrencyCode.Text)
		}
		if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo) {
			assert.Equal(t, "my condition", mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo.LoanCondition.Text)
		}
	}
	if assert.Len(t, mockPrRepo.savedNotifications, 1) {
		n := mockPrRepo.savedNotifications[0]
		assert.Equal(t, pr_db.NotificationDirectionSent, n.Direction)
		assert.Equal(t, pr_db.NotificationKindCondition, n.Kind)
		assert.Equal(t, "ISIL:SUP1", n.FromSymbol)
		assert.Equal(t, "ISIL:REQ1", n.ToSymbol)
		assert.Equal(t, "Condition note", n.Note.String)
		assert.Equal(t, "my condition", n.Condition.String)
		assert.Equal(t, "DKK", n.Currency.String)
		cost, err := n.Cost.Float64Value()
		assert.NoError(t, err)
		assert.Equal(t, 12.34, cost.Float64)
	}
}

func TestHandleInvokeLenderActionAddConditionMissingCurrency(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"loanCondition": "my condition",
			"note":          "Condition note",
			"cost":          12.34,
		},
	}})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.Equal(t, "currency is required when cost is provided", resultData.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
}

func TestHandleInvokeLenderActionAddConditionTypeCost(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"loanCondition": "my condition",
			"note":          "Condition note",
			"cost":          "12.34", // string instead of number
		},
	}})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.Equal(t, "failed to unmarshal action parameters", resultData.EventError.Message)
	assert.Contains(t, resultData.EventError.Cause, "cannot unmarshal")
	assert.Contains(t, resultData.EventError.Cause, "cost")
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
}

func TestHandleInvokeLenderActionShipOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CheckOutItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("", nil) // no title
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
		{
			ID:        "item1",
			PrID:      patronRequestId,
			Barcode:   "1234",
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		},
		{
			ID:        "item2",
			PrID:      patronRequestId,
			Barcode:   "5678",
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		},
	}, nil)

	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note": "my note",
		},
	}})
	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateShipped, mockPrRepo.savedPr.State)
	assert.Len(t, mockPrRepo.savedItems, 0)
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusLoaned, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "my note\n#MultipleItems#\n1234||\n5678||\n#MultipleItemsEnd#", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.LastChange.IsZero())
		if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo) {
			assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo.DateSent.IsZero())
		}
	}
}

func TestHandleInvokeLenderActionShipNewTitleOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CheckOutItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("new title", nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
		{
			ID:        "item1",
			PrID:      patronRequestId,
			Barcode:   "1234",
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		},
		{
			ID:        "item2",
			PrID:      patronRequestId,
			Barcode:   "5678",
			CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		},
	}, nil)

	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateShipped, mockPrRepo.savedPr.State)
	assert.Len(t, mockPrRepo.savedItems, 2)
	assert.Equal(t, "item1", mockPrRepo.savedItems[0].ID)
	assert.Equal(t, "1234", mockPrRepo.savedItems[0].Barcode)
	assert.Equal(t, "new title", mockPrRepo.savedItems[0].Title.String)
	assert.Equal(t, "item2", mockPrRepo.savedItems[1].ID)
	assert.Equal(t, "5678", mockPrRepo.savedItems[1].Barcode)
	assert.Equal(t, "new title", mockPrRepo.savedItems[1].Title.String)
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		assert.Equal(t, iso18626.TypeStatusLoaned, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
		assert.Equal(t, "#MultipleItems#\n1234||new title\n5678||new title\n#MultipleItemsEnd#", mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.Note)
		assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.LastChange.IsZero())
		if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo) {
			assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.DeliveryInfo.DateSent.IsZero())
		}
	}
}

func TestHandleInvokeLenderActionShipNewTitleFail(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CheckOutItem", mock.Anything, "1234", mock.Anything, mock.Anything).Return("new title", nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	mockPrRepo.saveItemFail = true

	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to save item", resultData.EventError.Message)
	assert.Equal(t, "db error", resultData.EventError.Cause)
}

func TestHandleInvokeLenderActionShipGetItemsByIdFail(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	action := LenderActionShip
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateShippedReturn, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	action := LenderActionMarkReceived
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateCompleted, mockPrRepo.savedPr.State)
}

func TestHandleInvokeLenderActionMarkReceivedNoItems(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateShippedReturn, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateShippedReturn, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	action := LenderActionMarkReceived
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CheckInItem failed", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionAcceptCancel(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateCancelRequested, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("req-1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	action := LenderActionAcceptCancel

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateCancelled, mockPrRepo.savedPr.State)
	lmsAdapter.AssertExpectations(t)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, iso18626.TypeReasonForMessageCancelResponse, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, iso18626.TypeStatusCancelled, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
	assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.Header.Timestamp.IsZero())
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo) {
		assert.Equal(t, iso18626.TypeYesNoY, *mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo)
	}
}

func TestHandleInvokeLenderActionAcceptCancelCancelRequestItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(errors.New("cancel failed"))
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateCancelRequested, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("req-1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	action := LenderActionAcceptCancel

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CancelRequestItem failed", resultData.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, LenderStateCancelRequested, mockPrRepo.savedPr.State)
	assert.False(t, mockPrRepo.savedPr.TerminalState)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionAcceptCancelMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateCancelRequested, Side: SideLending, RequesterSymbol: pgtype.Text{Valid: false, String: ""}, SupplierSymbol: getDbText("ISIL:SUP1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	action := LenderActionAcceptCancel

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "invalid requester symbol", resultData.EventError.Message)
}

func TestGetDirectoryEmailData(t *testing.T) {
	fromEmail := "from@example.com"
	toEmail := "to@example.com"
	emptyStr := ""

	tests := []struct {
		name           string
		symbol         string
		toNeeded       bool
		peer           ill_db.Peer
		repoErr        error
		wantFrom       string
		wantTo         *string
		wantErrMessage string
	}{
		{
			name:           "repo error",
			symbol:         "ISIL:A",
			toNeeded:       false,
			peer:           ill_db.Peer{},
			repoErr:        errors.New("db error"),
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "db error",
		},
		{
			name:           "fromEmail nil",
			symbol:         "ISIL:A",
			toNeeded:       false,
			peer:           ill_db.Peer{CustomData: directory.Entry{}},
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "from email is not configured",
		},
		{
			name:           "fromEmail empty string",
			symbol:         "ISIL:A",
			toNeeded:       false,
			peer:           ill_db.Peer{CustomData: directory.Entry{FromEmail: &emptyStr}},
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "from email is not configured",
		},
		{
			name:           "toNeeded true but email nil",
			symbol:         "ISIL:A",
			toNeeded:       true,
			peer:           ill_db.Peer{CustomData: directory.Entry{FromEmail: &fromEmail}},
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "email is not configured",
		},
		{
			name:           "toNeeded true but email empty",
			symbol:         "ISIL:A",
			toNeeded:       true,
			peer:           ill_db.Peer{CustomData: directory.Entry{FromEmail: &fromEmail, Email: &emptyStr}},
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "email is not configured",
		},
		{
			name:     "toNeeded false with no email configured",
			symbol:   "ISIL:A",
			toNeeded: false,
			peer:     ill_db.Peer{CustomData: directory.Entry{FromEmail: &fromEmail}},
			wantFrom: fromEmail,
			wantTo:   nil,
		},
		{
			name:     "toNeeded true with both emails configured",
			symbol:   "ISIL:A",
			toNeeded: true,
			peer:     ill_db.Peer{CustomData: directory.Entry{FromEmail: &fromEmail, Email: &toEmail}},
			wantFrom: fromEmail,
			wantTo:   &toEmail,
		},
		{
			name:     "toNeeded false with both emails configured",
			symbol:   "ISIL:A",
			toNeeded: false,
			peer:     ill_db.Peer{CustomData: directory.Entry{FromEmail: &fromEmail, Email: &toEmail}},
			wantFrom: fromEmail,
			wantTo:   &toEmail,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			illRepoMock := new(IllRepoMock)
			illRepoMock.On("GetPeerBySymbol", tc.symbol).Return(tc.peer, tc.repoErr)
			prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), illRepoMock, *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock))

			gotFrom, gotTo, err := prAction.getDirectoryEmailData(appCtx, tc.symbol, tc.toNeeded)

			if tc.wantErrMessage != "" {
				assert.EqualError(t, err, tc.wantErrMessage)
				assert.Equal(t, tc.wantFrom, gotFrom)
				assert.Equal(t, tc.wantTo, gotTo)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tc.wantFrom, gotFrom)
				assert.Equal(t, tc.wantTo, gotTo)
			}
		})
	}
}

func makeAddress(addrType string, addrData string) iso18626.Address {
	return iso18626.Address{
		ElectronicAddress: &iso18626.ElectronicAddress{
			ElectronicAddressType: iso18626.TypeSchemeValuePair{Text: addrType},
			ElectronicAddressData: addrData,
		},
	}
}

func makePhysicalAddress() iso18626.Address {
	return iso18626.Address{
		PhysicalAddress: &iso18626.PhysicalAddress{Line1: "123 Main St"},
	}
}

func TestPatronEmail(t *testing.T) {
	emailType := string(iso18626.ElectronicAddressTypeEmail)

	tests := []struct {
		name     string
		pr       pr_db.PatronRequest
		expected []string
	}{
		{
			name:     "nil PatronInfo returns empty",
			pr:       pr_db.PatronRequest{},
			expected: nil,
		},
		{
			name: "PatronInfo with no addresses returns empty",
			pr: pr_db.PatronRequest{
				IllRequest: iso18626.Request{
					PatronInfo: &iso18626.PatronInfo{},
				},
			},
			expected: nil,
		},
		{
			name: "address with nil ElectronicAddress is skipped",
			pr: pr_db.PatronRequest{
				IllRequest: iso18626.Request{
					PatronInfo: &iso18626.PatronInfo{
						Address: []iso18626.Address{makePhysicalAddress()},
					},
				},
			},
			expected: nil,
		},
		{
			name: "address with empty ElectronicAddressData is skipped",
			pr: pr_db.PatronRequest{
				IllRequest: iso18626.Request{
					PatronInfo: &iso18626.PatronInfo{
						Address: []iso18626.Address{makeAddress(emailType, "")},
					},
				},
			},
			expected: nil,
		},
		{
			name: "address with non-email ElectronicAddressType is skipped",
			pr: pr_db.PatronRequest{
				IllRequest: iso18626.Request{
					PatronInfo: &iso18626.PatronInfo{
						Address: []iso18626.Address{makeAddress("Ftp", "ftp://example.com")},
					},
				},
			},
			expected: nil,
		},
		{
			name: "single email address is returned",
			pr: pr_db.PatronRequest{
				IllRequest: iso18626.Request{
					PatronInfo: &iso18626.PatronInfo{
						Address: []iso18626.Address{makeAddress(emailType, "patron@example.com")},
					},
				},
			},
			expected: []string{"patron@example.com"},
		},
		{
			name: "multiple email addresses are all returned",
			pr: pr_db.PatronRequest{
				IllRequest: iso18626.Request{
					PatronInfo: &iso18626.PatronInfo{
						Address: []iso18626.Address{
							makeAddress(emailType, "first@example.com"),
							makeAddress(emailType, "second@example.com"),
						},
					},
				},
			},
			expected: []string{"first@example.com", "second@example.com"},
		},
		{
			name: "mix of email and non-email addresses returns only emails",
			pr: pr_db.PatronRequest{
				IllRequest: iso18626.Request{
					PatronInfo: &iso18626.PatronInfo{
						Address: []iso18626.Address{
							makeAddress("Ftp", "ftp://example.com"),
							makeAddress(emailType, "patron@example.com"),
							makePhysicalAddress(),
						},
					},
				},
			},
			expected: []string{"patron@example.com"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := patronEmail(tc.pr)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func newActionServiceWithEmail(prRepo *MockPrRepo, emailSvc *EmailSenderMock) *PatronRequestActionService {
	return CreatePatronRequestActionService(
		prRepo,
		new(IllRepoMock),
		*new(events.EventBus),
		new(handler.Iso18626Handler),
		nil,
		emailSvc,
	)
}

func TestCreateAndSendEmail(t *testing.T) {
	const symbol = "ISIL:TEST"
	const from = "sender@example.com"
	recipients := []string{"patron@example.com"}
	const label = "test-label"
	const audience = proapi.ModelActionParamsSendToPatron

	foundTemplate := pr_db.Template{
		Body:    "Hello patron",
		Subject: pgtype.Text{String: "Your request", Valid: true},
	}

	tests := []struct {
		name          string
		from          string
		recipients    []string
		setupPrRepo   func(m *MockPrRepo)
		setupEmail    func(m *EmailSenderMock)
		assertEmail   func(t *testing.T, m *EmailSenderMock)
		wantErrSubstr string
	}{
		{
			name:       "success – template found, email built and sent",
			from:       from,
			recipients: recipients,
			setupPrRepo: func(m *MockPrRepo) {
				m.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
			},
			setupEmail: func(m *EmailSenderMock) {
				m.On("SendEmail", from).Return(nil)
			},
			assertEmail: func(t *testing.T, m *EmailSenderMock) {
				m.AssertCalled(t, "SendEmail", from)
			},
		},
		{
			name:       "template not found returns error",
			from:       from,
			recipients: recipients,
			setupPrRepo: func(m *MockPrRepo) {
				m.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{}, errors.New("no template found"))
			},
			setupEmail: func(m *EmailSenderMock) {},
			assertEmail: func(t *testing.T, m *EmailSenderMock) {
				m.AssertNotCalled(t, "SendEmail", mock.Anything)
			},
			wantErrSubstr: "no template found",
		},
		{
			name:       "SendEmail error is propagated",
			from:       from,
			recipients: recipients,
			setupPrRepo: func(m *MockPrRepo) {
				m.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
			},
			setupEmail: func(m *EmailSenderMock) {
				m.On("SendEmail", from).Return(errors.New("smtp failure"))
			},
			assertEmail: func(t *testing.T, m *EmailSenderMock) {
				m.AssertCalled(t, "SendEmail", from)
			},
			wantErrSubstr: "smtp failure",
		},
		{
			name:       "header injection in from triggers BuildRawMessage error",
			from:       "bad\r\nfrom@example.com",
			recipients: recipients,
			setupPrRepo: func(m *MockPrRepo) {
				m.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
			},
			setupEmail: func(m *EmailSenderMock) {},
			assertEmail: func(t *testing.T, m *EmailSenderMock) {
				m.AssertNotCalled(t, "SendEmail", mock.Anything)
			},
			wantErrSubstr: "header injection",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			tc.setupPrRepo(mockPrRepo)
			mockEmail := new(EmailSenderMock)
			tc.setupEmail(mockEmail)
			svc := newActionServiceWithEmail(mockPrRepo, mockEmail)

			err := svc.createAndSendEmail(appCtx, symbol, tc.from, tc.recipients, label, audience)

			if tc.wantErrSubstr == "" {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, tc.wantErrSubstr)
			}
			tc.assertEmail(t, mockEmail)
		})
	}
}

// helpers for sendEmailNotification tests

func ptr[T any](v T) *T { return &v }

func peerWithEmail(fromEmail, toEmail string) ill_db.Peer {
	return ill_db.Peer{CustomData: directory.Entry{
		FromEmail: ptr(fromEmail),
		Email:     ptr(toEmail),
	}}
}

func peerWithFromEmailOnly(fromEmail string) ill_db.Peer {
	return ill_db.Peer{CustomData: directory.Entry{
		FromEmail: ptr(fromEmail),
	}}
}

func prWithPatronEmail(patronAddr string) pr_db.PatronRequest {
	return pr_db.PatronRequest{
		IllRequest: iso18626.Request{
			PatronInfo: &iso18626.PatronInfo{
				Address: []iso18626.Address{makeAddress(string(iso18626.ElectronicAddressTypeEmail), patronAddr)},
			},
		},
	}
}

const (
	testSymbol   = "ISIL:TEST"
	testFrom     = "from@example.com"
	testStaffTo  = "staff@example.com"
	testPatronTo = "patron@example.com"
	testTemplate = "notify-template"
)

func sendToTargets(targets ...proapi.ModelActionParamsSendTo) *[]proapi.ModelActionParamsSendTo {
	s := targets
	return &s
}

func autoParams(tmpl string, targets ...proapi.ModelActionParamsSendTo) actionParams {
	return actionParams{
		AutoActionParams: &proapi.ModelAction_Params{
			TemplateLabel: ptr(tmpl),
			SendTo:        sendToTargets(targets...),
		},
	}
}

func TestSendEmailNotification(t *testing.T) {
	foundTemplate := pr_db.Template{
		Body:    "Hello",
		Subject: pgtype.Text{String: "Subject", Valid: true},
	}

	tests := []struct {
		name       string
		pr         pr_db.PatronRequest
		params     actionParams
		symbol     string
		setupMocks func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock)
		wantStatus events.EventStatus
		wantNote   string
		wantErr    string
	}{
		{
			name:       "no AutoActionParams – success with empty result",
			pr:         pr_db.PatronRequest{},
			params:     actionParams{},
			symbol:     testSymbol,
			setupMocks: func(_ *MockPrRepo, _ *IllRepoMock, _ *EmailSenderMock) {},
			wantStatus: events.EventStatusSuccess,
		},
		{
			name:       "nil SendTo – success with empty result",
			pr:         pr_db.PatronRequest{},
			params:     actionParams{AutoActionParams: &proapi.ModelAction_Params{}},
			symbol:     testSymbol,
			setupMocks: func(_ *MockPrRepo, _ *IllRepoMock, _ *EmailSenderMock) {},
			wantStatus: events.EventStatusSuccess,
		},
		{
			name:       "empty SendTo – success with empty result",
			pr:         pr_db.PatronRequest{},
			params:     actionParams{AutoActionParams: &proapi.ModelAction_Params{SendTo: sendToTargets()}},
			symbol:     testSymbol,
			setupMocks: func(_ *MockPrRepo, _ *IllRepoMock, _ *EmailSenderMock) {},
			wantStatus: events.EventStatusSuccess,
		},
		{
			name:   "nil TemplateLabel – error",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: actionParams{AutoActionParams: &proapi.ModelAction_Params{
				SendTo: sendToTargets(proapi.ModelActionParamsSendToPatron),
			}},
			setupMocks: func(_ *MockPrRepo, _ *IllRepoMock, _ *EmailSenderMock) {},
			wantStatus: events.EventStatusError,
			wantErr:    "template label is not set",
		},
		{
			name:   "GetPeerBySymbol error – error result",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToPatron),
			setupMocks: func(_ *MockPrRepo, illRepo *IllRepoMock, _ *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(ill_db.Peer{}, errors.New("db error"))
			},
			wantStatus: events.EventStatusError,
			wantErr:    "error getting directory email data",
		},
		{
			name:   "SendTo patron – no patron email addresses – note set",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToPatron),
			setupMocks: func(_ *MockPrRepo, illRepo *IllRepoMock, _ *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithFromEmailOnly(testFrom), nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "no recipients found for patron",
		},
		{
			name:   "SendTo patron – email sent successfully",
			pr:     prWithPatronEmail(testPatronTo),
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToPatron),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithFromEmailOnly(testFrom), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "patron email sent successfully",
		},
		{
			name:   "SendTo patron – SendEmail fails – error result",
			pr:     prWithPatronEmail(testPatronTo),
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToPatron),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithFromEmailOnly(testFrom), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(errors.New("smtp error"))
			},
			wantStatus: events.EventStatusError,
			wantErr:    "error sending email to patron",
		},
		{
			name:   "SendTo staff – email sent successfully",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithEmail(testFrom, testStaffTo), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "staff email sent successfully",
		},
		{
			name:   "SendTo staff – multiple semicolon-separated addresses all sent",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithEmail(testFrom, "a@example.com; b@example.com"), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "staff email sent successfully",
		},
		{
			name:   "SendTo staff – trailing semicolon is ignored, email sent",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithEmail(testFrom, testStaffTo+";"), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "staff email sent successfully",
		},
		{
			name:   "SendTo staff – whitespace-only address yields no recipients",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(_ *MockPrRepo, illRepo *IllRepoMock, _ *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithEmail(testFrom, "  ; ; "), nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "no recipients found for staff",
		},
		{
			name:   "SendTo staff – SendEmail fails – error result",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithEmail(testFrom, testStaffTo), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(errors.New("smtp error"))
			},
			wantStatus: events.EventStatusError,
			wantErr:    "error sending email to staff",
		},
		{
			name:   "SendTo patron and staff – both emails sent – staff note wins",
			pr:     prWithPatronEmail(testPatronTo),
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToPatron, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithEmail(testFrom, testStaffTo), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "staff email sent successfully",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prRepo := new(MockPrRepo)
			illRepo := new(IllRepoMock)
			emailSvc := new(EmailSenderMock)
			tc.setupMocks(prRepo, illRepo, emailSvc)

			svc := CreatePatronRequestActionService(
				prRepo,
				illRepo,
				*new(events.EventBus),
				new(handler.Iso18626Handler),
				nil,
				emailSvc,
			)

			res := svc.sendEmailNotification(appCtx, tc.pr, tc.params, tc.symbol)

			assert.Equal(t, tc.wantStatus, res.status)
			if tc.wantErr != "" {
				if assert.NotNil(t, res.result) {
					assert.Contains(t, res.result.EventError.Message, tc.wantErr)
				}
			}
			if tc.wantNote != "" {
				if assert.NotNil(t, res.result) {
					assert.Equal(t, tc.wantNote, res.result.Note)
				}
			}
			illRepo.AssertExpectations(t)
			emailSvc.AssertExpectations(t)
		})
	}
}

func TestHandleInvokeActionBorrowerActionSendNotification(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(true)
	emailMock.On("SendEmail", mock.Anything).Return(nil)
	illMock := new(IllRepoMock)
	illMock.On("GetPeerBySymbol", "ISIL:REC1").Return(ill_db.Peer{
		CustomData: directory.Entry{
			Email:     ptr("staff@mail.com"),
			FromEmail: ptr("from@mail.com"),
		},
	}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{Body: "body", Subject: pgtype.Text{String: "subj", Valid: true}}, nil)

	action := BorrowerActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToPatron, proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("shipped-template"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateShipped, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionBorrowerActionSendNotification_emailServiceNotReady(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(false)
	illMock := new(IllRepoMock)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToPatron, proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("shipped-template"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, "email service is not ready to send", resultData.Note)
	assert.Equal(t, BorrowerStateShipped, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionLenderActionSendNotification(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(true)
	emailMock.On("SendEmail", mock.Anything).Return(nil)
	illMock := new(IllRepoMock)
	illMock.On("GetPeerBySymbol", "ISIL:SUP1").Return(ill_db.Peer{
		CustomData: directory.Entry{
			Email:     ptr("staff@mail.com"),
			FromEmail: ptr("from@mail.com"),
		},
	}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{Body: "body", Subject: pgtype.Text{String: "subj", Valid: true}}, nil)

	action := LenderActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToPatron, proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("validated-template"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionLenderActionSendNotification_emailServiceNotReady(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(false)
	illMock := new(IllRepoMock)
	illMock.On("GetPeerBySymbol", "ISIL:SUP1").Return(ill_db.Peer{
		CustomData: directory.Entry{
			Email:     ptr("staff@mail.com"),
			FromEmail: ptr("from@mail.com"),
		},
	}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := LenderActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToPatron, proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("validated-template"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, "email service is not ready to send", resultData.Note)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
}

func TestHandleInvokeBorrowerActionCancelLocalSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           BorrowerStateLocalSupply,
		Side:            SideBorrowing,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		RequesterReqID:  getDbText("req-1"),
	}, nil)
	action := BorrowerActionCancelLocalSupply

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateCancelled, mockPrRepo.savedPr.State)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, iso18626.TypeStatusCancelled, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
	assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.Header.Timestamp.IsZero())
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.AnswerYesNo)
}

func TestHandleInvokeBorrowerActionCannotSupplyLocally(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           BorrowerStateLocalSupply,
		Side:            SideBorrowing,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		RequesterReqID:  getDbText("req-1"),
	}, nil)
	action := BorrowerActionCannotSupplyLocally

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
	assert.Equal(t, iso18626.TypeStatusUnfilled, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
	assert.False(t, mockIso18626Handler.lastSupplyingAgencyMessage.Header.Timestamp.IsZero())
}

func TestHandleInvokeBorrowerActionFillLocally(t *testing.T) {
	tests := []struct {
		name           string
		serviceType    iso18626.TypeServiceType
		manualAdapter  bool
		expectedStatus iso18626.TypeStatus
	}{
		{name: "loan", serviceType: iso18626.TypeServiceTypeLoan, expectedStatus: iso18626.TypeStatusLoanCompleted},
		{name: "copy", serviceType: iso18626.TypeServiceTypeCopy, expectedStatus: iso18626.TypeStatusCopyCompleted},
		{name: "NCIP disabled", serviceType: iso18626.TypeServiceTypeLoan, manualAdapter: true, expectedStatus: iso18626.TypeStatusLoanCompleted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			lmsCreator := new(MockLmsCreator)
			mockIso18626Handler := new(MockIso18626Handler)
			illRequest := iso18626.Request{
				BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "local-record-1"},
				ServiceInfo:       &iso18626.ServiceInfo{ServiceType: tt.serviceType},
			}
			pr := pr_db.PatronRequest{
				ID:              patronRequestId,
				IllRequest:      illRequest,
				State:           BorrowerStateLocalSupply,
				Side:            SideBorrowing,
				Patron:          getDbText("patron-1"),
				RequesterSymbol: getDbText("ISIL:REQ1"),
				SupplierSymbol:  getDbText("ISIL:REQ1"),
				RequesterReqID:  getDbText("req-1"),
				NeedsAttention:  true,
			}

			var lmsAdapter lms.LmsAdapter
			if tt.manualAdapter {
				lmsAdapter = &lms.LmsAdapterManual{}
			} else {
				adapterMock := &mockLmsAdapter{
					requesterPickupLocation: "pickup-1",
					itemLocation:            "item-location-1",
				}
				adapterMock.On("RequestItem", patronRequestId, "local-record-1", "patron-1", "pickup-1", "item-location-1").
					Return("", "", "", nil)
				lmsAdapter = adapterMock
			}
			lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(lmsAdapter, nil)
			mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
			prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock))
			action := BorrowerActionFillLocally

			status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
				PatronRequestID: patronRequestId,
				EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
			})

			assert.Equal(t, events.EventStatusSuccess, status)
			if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ToState) {
				assert.Equal(t, string(BorrowerStateCompleted), *resultData.ActionResult.ToState)
			}
			assert.Equal(t, BorrowerStateCompleted, mockPrRepo.savedPr.State)
			assert.True(t, mockPrRepo.savedPr.TerminalState)
			assert.False(t, mockPrRepo.savedPr.NeedsAttention)
			if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
				assert.Equal(t, tt.expectedStatus, mockIso18626Handler.lastSupplyingAgencyMessage.StatusInfo.Status)
				assert.Equal(t, iso18626.TypeReasonForMessageStatusChange, mockIso18626Handler.lastSupplyingAgencyMessage.MessageInfo.ReasonForMessage)
			}
			if adapterMock, ok := lmsAdapter.(*mockLmsAdapter); ok {
				adapterMock.AssertExpectations(t)
			}
		})
	}
}

type MockEventBus struct {
	mock.Mock
	events.EventBus
	createdTaskData     []events.EventData
	createdTaskIDs      []string
	createdTaskNames    []events.EventName
	createdNoticeIDs    []string
	createdNoticeData   []events.EventData
	createdNoticeStatus []events.EventStatus
	processedTaskEvents []events.Event
	createTaskErr       error
	runTaskHandler      bool
}

func (m *MockEventBus) ProcessTask(ctx common.ExtendedContext, event events.Event, target events.SignalTarget, h func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	if m.runTaskHandler {
		status, result := h(ctx, event)
		event.EventStatus = status
		if result != nil {
			event.ResultData = *result
		}
		return event, nil
	}
	for _, call := range m.ExpectedCalls {
		if call.Method == "ProcessTask" {
			args := m.Called(event.ID)
			return args.Get(0).(events.Event), args.Error(1)
		}
	}
	status, result := h(ctx, event)
	event.EventStatus = status
	if result != nil {
		event.ResultData = *result
	}
	m.processedTaskEvents = append(m.processedTaskEvents, event)
	return event, nil
}

func (m *MockEventBus) ProcessExclusiveTask(ctx common.ExtendedContext, event events.Event, target events.SignalTarget, h func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	if m.runTaskHandler {
		status, result := h(ctx, event)
		event.EventStatus = status
		if result != nil {
			event.ResultData = *result
		}
		return event, nil
	}
	for _, call := range m.ExpectedCalls {
		if call.Method == "ProcessExclusiveTask" {
			args := m.Called(event.ID)
			return args.Get(0).(events.Event), args.Error(1)
		}
	}
	status, result := h(ctx, event)
	event.EventStatus = status
	if result != nil {
		event.ResultData = *result
	}
	m.processedTaskEvents = append(m.processedTaskEvents, event)
	return event, nil
}

func (m *MockEventBus) CreateTask(id string, eventName events.EventName, data events.EventData, eventClass events.EventDomain, parentId *string, target events.SignalTarget) (string, error) {
	m.createdTaskData = append(m.createdTaskData, data)
	m.createdTaskNames = append(m.createdTaskNames, eventName)
	if m.createTaskErr != nil {
		return "", m.createTaskErr
	}
	if id == "error" {
		return "", errors.New("event bus error")
	}
	taskID := fmt.Sprintf("%s-task-%d", id, len(m.createdTaskData))
	m.createdTaskIDs = append(m.createdTaskIDs, taskID)
	return taskID, nil
}

func (m *MockEventBus) CreateNotice(id string, eventName events.EventName, data events.EventData, status events.EventStatus, eventDomain events.EventDomain, target events.SignalTarget) (string, error) {
	m.createdNoticeIDs = append(m.createdNoticeIDs, id)
	m.createdNoticeData = append(m.createdNoticeData, data)
	m.createdNoticeStatus = append(m.createdNoticeStatus, status)
	if id == "error" {
		return "", errors.New("event bus error")
	}
	return id, nil
}

func (m *MockEventBus) CreateNoticeWithParent(id string, eventName events.EventName, data events.EventData, status events.EventStatus, eventDomain events.EventDomain, parentId *string, target events.SignalTarget) (string, error) {
	if parentId == nil || id == "error" {
		return "", errors.New("event bus error")
	}
	m.createdNoticeIDs = append(m.createdNoticeIDs, id)
	m.createdNoticeData = append(m.createdNoticeData, data)
	m.createdNoticeStatus = append(m.createdNoticeStatus, status)
	for _, call := range m.ExpectedCalls {
		if call.Method == "CreateNoticeWithParent" {
			args := m.Called(*parentId)
			return args.Get(0).(string), args.Error(1)
		}
	}
	return id, nil
}

type MockPrRepo struct {
	mock.Mock
	pr_db.PgPrRepo
	savedPr                              pr_db.PatronRequest
	createdPr                            pr_db.PatronRequest
	savedItems                           []pr_db.Item
	savedNotifications                   []pr_db.Notification
	markedConditionNotificationsReceipts []pr_db.MarkConditionNotificationsReceiptParams
	saveItemFail                         bool
}

func (r *MockPrRepo) WithTxFunc(ctx common.ExtendedContext, fn func(repo pr_db.PrRepo) error) error {
	return fn(r)
}

func (r *MockPrRepo) GetPatronRequestById(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
	for _, call := range r.ExpectedCalls {
		if call.Method == "GetPatronRequestById" {
			args := r.Called(id)
			return args.Get(0).(pr_db.PatronRequest), args.Error(1)
		}
	}
	if r.savedPr.ID == id {
		return r.savedPr, nil
	}
	return pr_db.PatronRequest{}, errors.New("db error")
}

func (r *MockPrRepo) GetPatronRequestByIdForUpdate(ctx common.ExtendedContext, id string) (pr_db.PatronRequest, error) {
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
	return r.savedPr, nil
}

func (r *MockPrRepo) CreatePatronRequest(ctx common.ExtendedContext, params pr_db.CreatePatronRequestParams) (pr_db.PatronRequest, error) {
	if strings.Contains(params.ID, "error") || strings.Contains(params.RequesterReqID.String, "error") {
		return pr_db.PatronRequest{}, errors.New("db error")
	}
	r.savedPr = pr_db.PatronRequest(params)
	r.createdPr = r.savedPr
	return r.savedPr, nil
}

func (r *MockPrRepo) GetNextHrid(ctx common.ExtendedContext, prefix string) (string, error) {
	return strings.ToUpper(prefix) + "-2", nil
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

func (r *MockPrRepo) GetNotificationsByPrId(ctx common.ExtendedContext, params pr_db.GetNotificationsByPrIdParams) ([]pr_db.Notification, int64, error) {
	notifications := make([]pr_db.Notification, 0, len(r.savedNotifications))
	for _, notification := range r.savedNotifications {
		if notification.PrID != params.PrID {
			continue
		}
		if params.Kind != "" && string(notification.Kind) != params.Kind {
			continue
		}
		notifications = append(notifications, notification)
	}
	fullCount := int64(len(notifications))
	if params.Offset >= int32(len(notifications)) {
		return nil, fullCount, nil
	}
	end := params.Offset + params.Limit
	if end > int32(len(notifications)) {
		end = int32(len(notifications))
	}
	return notifications[params.Offset:end], fullCount, nil
}

func (r *MockPrRepo) MarkConditionNotificationsReceipt(ctx common.ExtendedContext, params pr_db.MarkConditionNotificationsReceiptParams) error {
	r.markedConditionNotificationsReceipts = append(r.markedConditionNotificationsReceipts, params)
	if params.PrID == "error" {
		return errors.New("db error")
	}
	return nil
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

func (r *MockPrRepo) GetNotificationById(ctx common.ExtendedContext, id string) (pr_db.Notification, error) {
	args := r.Called(id)
	return args.Get(0).(pr_db.Notification), args.Error(1)
}

func (r *MockPrRepo) GetTemplateByPurposeAudienceLabelAndOwner(ctx common.ExtendedContext, params pr_db.GetTemplateByPurposeAudienceLabelAndOwnerParams) (pr_db.Template, error) {
	args := r.Called(params)
	return args.Get(0).(pr_db.Template), args.Error(1)
}

type MockIso18626Handler struct {
	mock.Mock
	handler.Iso18626Handler
	lastRequestingAgencyMessage *iso18626.RequestingAgencyMessage
	lastSupplyingAgencyMessage  *iso18626.SupplyingAgencyMessage
}

func (h *MockIso18626Handler) HandleRequest(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	status := iso18626.TypeMessageStatusOK
	if illMessage.Request.Header.RequestingAgencyRequestId == "error" {
		status = iso18626.TypeMessageStatusERROR
	}
	var resmsg = iso18626.NewISO18626Message()
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{
			MessageStatus: status,
		},
	}
	if illMessage.Request.Header.RequestingAgencyRequestId == "duplicate" {
		resmsg.RequestConfirmation.ConfirmationHeader.MessageStatus = iso18626.TypeMessageStatusERROR
		resmsg.RequestConfirmation.ErrorData = &iso18626.ErrorData{
			ErrorType:  iso18626.TypeErrorTypeUnrecognisedDataValue,
			ErrorValue: string(handler.ReqIsDuplicate),
		}
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
	h.lastRequestingAgencyMessage = illMessage.RequestingAgencyMessage
	status := iso18626.TypeMessageStatusOK
	if illMessage.RequestingAgencyMessage.Header.RequestingAgencyRequestId == "error" {
		status = iso18626.TypeMessageStatusERROR
	}
	var resmsg = iso18626.NewISO18626Message()
	resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{
			MessageStatus: status,
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
	var resmsg = iso18626.NewISO18626Message()
	resmsg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: iso18626.ConfirmationHeader{
			MessageStatus: status,
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
) (string, string, string, error) {
	return "", "", "", nil
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
) (string, string, string, error) {
	return "", "", "", errors.New("RequestItem failed")
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

type mockLmsAdapter struct {
	mock.Mock
	lms.LmsAdapterManual
	requesterPickupLocation string
	itemLocation            string
}

func (m *mockLmsAdapter) RequesterPickupLocation() string {
	return m.requesterPickupLocation
}

func (m *mockLmsAdapter) ItemLocation() string {
	return m.itemLocation
}

func (m *mockLmsAdapter) CancelRequestItem(requestId string, userId string) error {
	args := m.Called(requestId, userId)
	return args.Error(0)
}

func (m *mockLmsAdapter) CheckOutItem(
	requestId string,
	itemBarcode string,
	userId string,
	externalReferenceValue string,
) (string, error) {
	args := m.Called(requestId, itemBarcode, userId, externalReferenceValue)
	return args.String(0), args.Error(1)
}

func (m *mockLmsAdapter) RequestItem(
	requestId string,
	itemId string,
	userId string,
	pickupLocation string,
	itemLocation string,
) (barcode string, callNumber string, title string, err error) {
	args := m.Called(requestId, itemId, userId, pickupLocation, itemLocation)
	return args.String(0), args.String(1), args.String(2), args.Error(3)
}

func (m *mockLmsAdapter) AcceptItem(
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
	args := m.Called(itemId, requestId, userId, author, title, isbn, callNumber, pickupLocation, requestedAction)
	return args.Error(0)
}

type EmailSenderMock struct {
	mock.Mock
}

func (s *EmailSenderMock) IsReadyToSend() bool {
	return s.Called().Bool(0)
}

func (s *EmailSenderMock) SendEmail(from string, to []string, raw []byte) error {
	return s.Called(from).Error(0)
}

type IllRepoMock struct {
	ill_db.PgIllRepo
	mock.Mock
}

func (i *IllRepoMock) GetPeerBySymbol(ctx common.ExtendedContext, symbol string) (ill_db.Peer, error) {
	args := i.Called(symbol)
	return args.Get(0).(ill_db.Peer), args.Error(1)
}
