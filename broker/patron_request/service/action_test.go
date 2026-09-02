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

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/lms"
	"github.com/indexdata/crosslink/broker/ncipclient"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/shim"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/crosslink/ncip"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var appCtx = common.CreateExtCtxWithArgs(context.Background(), nil)
var patronRequestId = "pr1"

func TestLmsNoticeStatus(t *testing.T) {
	assert.Equal(t, events.EventStatusSuccess, lmsNoticeStatus(nil))
	assert.Equal(t, events.EventStatusProblem, lmsNoticeStatus(&ncipclient.NcipError{
		Problem: ncip.Problem{ProblemType: ncip.SchemeValuePair{Text: string(ncip.UnknownUser)}},
	}))
	assert.Equal(t, events.EventStatusError, lmsNoticeStatus(errors.New("transport failed")))
}

func TestCheckDuplicateBorrowingRequestUsesPatronRequests(t *testing.T) {
	windowHours := int32(24)
	createdAt := time.Date(2026, time.August, 25, 12, 30, 0, 123456000, time.UTC)
	illRepo := new(IllRepoMock)
	illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:REQ"}, mock.Anything).Return([]ill_db.Peer{{
		CustomData: dirapi.Entry{IllConfig: &dirapi.IllConfig{DuplicateCheckWindowHours: &windowHours}},
	}}, "", nil)
	prRepo := new(MockPrRepo)
	serviceInfo := &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}
	match := pr_db.PatronRequest{
		ID:         "matched-pr",
		IllRequest: iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "record-1"}, ServiceInfo: serviceInfo},
	}
	prRepo.On("ListPatronRequests", pr_db.ListPatronRequestsParams{Limit: 1, Offset: 0}, mock.Anything).Return([]pr_db.PatronRequest{match}, int64(1), nil)
	actionService := CreatePatronRequestActionService(prRepo, illRepo, new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              "current-pr",
		CreatedAt:       pgtype.Timestamp{Time: createdAt, Valid: true},
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REQ"),
		Patron:          getDbText("patron-1"),
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "record-1", Title: "A title"},
			ServiceInfo:       serviceInfo,
		},
	}

	got := actionService.checkDuplicateBorrowingRequest(appCtx, pr)

	assert.Equal(t, events.EventStatusSuccess, got.status)
	if assert.NotNil(t, got.result.ActionResult) {
		assert.Equal(t, ActionOutcomeReview, got.result.ActionResult.Outcome)
	}
	duplicateCheck, ok := got.result.CustomData[duplicateCheckKey].(*events.DuplicateCheck)
	if assert.True(t, ok) {
		assert.True(t, duplicateCheck.Enabled)
		assert.True(t, duplicateCheck.Duplicate)
		assert.Equal(t, createdAt.Add(-24*time.Hour).Format(time.RFC3339Nano), *duplicateCheck.CutoffTime)
		assert.Equal(t, "matched-pr", *duplicateCheck.MatchedPatronRequestId)
		assert.Nil(t, duplicateCheck.MatchedTransactionId)
	}
	where := prRepo.lastListQuery.GetWhereClause()
	assert.Contains(t, where, "requester_symbol")
	assert.Contains(t, where, "patron")
	assert.Contains(t, where, "created_at")
	assert.Contains(t, where, "created_at <")
	assert.Contains(t, where, "service_type")
	assert.Contains(t, where, "id NOT IN")
	queryArgs := prRepo.lastListQuery.GetQueryArguments()
	assert.Contains(t, queryArgs, "record-1")
	assert.Contains(t, queryArgs, "A title")
	assert.Contains(t, queryArgs, "borrowing")
	assert.Contains(t, queryArgs, "ISIL:REQ")
	assert.Contains(t, queryArgs, "patron-1")
	assert.Contains(t, queryArgs, createdAt.Add(-24*time.Hour))
	assert.Contains(t, queryArgs, createdAt)
	assert.Contains(t, queryArgs, "Loan")
	assert.Contains(t, queryArgs, "current-pr")
	prRepo.AssertExpectations(t)
	illRepo.AssertExpectations(t)
}

func TestCheckDuplicateBorrowingRequestSkipsRetry(t *testing.T) {
	actionService := CreatePatronRequestActionService(new(MockPrRepo), new(IllRepoMock), new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
	got := actionService.checkDuplicateBorrowingRequest(appCtx, pr_db.PatronRequest{PrevReqID: getDbText("previous-pr")})

	assert.Equal(t, events.EventStatusSuccess, got.status)
	assert.Nil(t, got.result.ActionResult)
	duplicateCheck := got.result.CustomData[duplicateCheckKey].(*events.DuplicateCheck)
	assert.False(t, duplicateCheck.Enabled)
}

func TestHandleInvokeCheckDuplicateTransitionsToDuplicate(t *testing.T) {
	windowHours := int32(24)
	illRepo := new(IllRepoMock)
	illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:REQ"}, mock.Anything).Return([]ill_db.Peer{{
		CustomData: dirapi.Entry{IllConfig: &dirapi.IllConfig{DuplicateCheckWindowHours: &windowHours}},
	}}, "", nil)
	prRepo := new(MockPrRepo)
	serviceInfo := &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}
	pr := pr_db.PatronRequest{
		ID:              "current-pr",
		CreatedAt:       pgtype.Timestamp{Time: time.Now(), Valid: true},
		State:           BorrowerStateMetadataUpdated,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REQ"),
		Patron:          getDbText("patron-1"),
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "record-1"},
			ServiceInfo:       serviceInfo,
		},
	}
	prRepo.On("GetPatronRequestById", pr.ID).Return(pr, nil)
	prRepo.On("ListPatronRequests", pr_db.ListPatronRequestsParams{Limit: 1, Offset: 0}, mock.Anything).Return([]pr_db.PatronRequest{{
		ID:         "matched-pr",
		IllRequest: pr.IllRequest,
	}}, int64(1), nil)
	actionService := CreatePatronRequestActionService(prRepo, illRepo, new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
	action := BorrowerActionCheckDuplicate

	status, result := actionService.handleInvokeAction(appCtx, events.Event{PatronRequestID: pr.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeReview, result.ActionResult.Outcome)
	assert.Equal(t, string(BorrowerStateDuplicate), *result.ActionResult.ToState)
	assert.Equal(t, BorrowerStateDuplicate, prRepo.savedPr.State)
	assert.True(t, prRepo.savedPr.NeedsAttention)
	assert.Equal(t, string(BorrowerActionCheckDuplicate), prRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeReview, prRepo.savedPr.LastActionOutcome.String)
	prRepo.AssertExpectations(t)
	illRepo.AssertExpectations(t)
}

func TestHandleInvokeCheckDuplicateFailureStaysMetadataUpdated(t *testing.T) {
	windowHours := int32(24)
	illRepo := new(IllRepoMock)
	illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:REQ"}, mock.Anything).Return([]ill_db.Peer{{
		CustomData: dirapi.Entry{IllConfig: &dirapi.IllConfig{DuplicateCheckWindowHours: &windowHours}},
	}}, "", nil)
	prRepo := new(MockPrRepo)
	serviceInfo := &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}
	pr := pr_db.PatronRequest{
		ID:              "current-pr",
		CreatedAt:       pgtype.Timestamp{Time: time.Now(), Valid: true},
		State:           BorrowerStateMetadataUpdated,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REQ"),
		Patron:          getDbText("patron-1"),
		IllRequest: iso18626.Request{
			BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "record-1"},
			ServiceInfo:       serviceInfo,
		},
	}
	prRepo.On("GetPatronRequestById", pr.ID).Return(pr, nil)
	prRepo.On("ListPatronRequests", pr_db.ListPatronRequestsParams{Limit: 1, Offset: 0}, mock.Anything).
		Return([]pr_db.PatronRequest{}, int64(0), errors.New("database unavailable"))
	actionService := CreatePatronRequestActionService(prRepo, illRepo, new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
	action := BorrowerActionCheckDuplicate

	status, result := actionService.handleInvokeAction(appCtx, events.Event{PatronRequestID: pr.ID, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to check for duplicate patron requests", result.EventError.Message)
	assert.Equal(t, ActionOutcomeFailure, result.ActionResult.Outcome)
	assert.Nil(t, result.ActionResult.ToState)
	duplicateCheck := result.CustomData[duplicateCheckKey].(*events.DuplicateCheck)
	assert.True(t, duplicateCheck.Enabled)
	assert.False(t, duplicateCheck.Duplicate)
	assert.Equal(t, BorrowerStateMetadataUpdated, prRepo.savedPr.State)
	assert.True(t, prRepo.savedPr.NeedsAttention)
	assert.Equal(t, string(BorrowerActionCheckDuplicate), prRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeFailure, prRepo.savedPr.LastActionOutcome.String)
	prRepo.AssertExpectations(t)
	illRepo.AssertExpectations(t)
}

var actionValidatePatron = BorrowerActionValidatePatron

func TestInvokeAction(t *testing.T) {
	mockEventBus := new(MockEventBus)
	prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), new(IllRepoMock), mockEventBus, new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	event := events.Event{
		ID: "action-1",
	}
	mockEventBus.On("ProcessExclusiveTask", event.ID).Return(event, nil)

	prAction.InvokeAction(appCtx, event)

	mockEventBus.AssertNumberOfCalls(t, "ProcessExclusiveTask", 1)
}

func TestHandleInvokeActionNotSpecifiedAction(t *testing.T) {
	prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "action not specified", resultData.EventError.Message)
}

func TestHandleInvokeActionNoPR(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, errors.New("not fund"))

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to read patron request", resultData.EventError.Message)
}

func TestHandleInvokeActionNoPRSide(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateNew, Side: "helper", IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "state NEW does not support action validate-patron", resultData.EventError.Message)
}

func TestHandleInvokeActionWhichIsNotAllowed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "state VALIDATED does not support action validate-patron", resultData.EventError.Message)
}

func TestHandleInvokeActionNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS creator not configured", resultData.EventError.Message)
}

func TestHandleInvokeActionTerminateUsesClosingAction(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
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
	assert.Equal(t, string(BorrowerActionCloseRequest), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeSuccess, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(events.EventStatusSuccess), mockPrRepo.savedPr.LastActionResult.String)
}

func TestHandleInvokeActionTerminateFallsBackWhenClosingActionFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	action := TerminateAction
	staleUpdatedAt := pgtype.Timestamp{Time: time.Now().Add(-time.Minute), Valid: true}
	currentUpdatedAt := pgtype.Timestamp{Time: time.Now(), Valid: true}
	pr := pr_db.PatronRequest{
		ID:             patronRequestId,
		State:          LenderStateValidated,
		Side:           SideLending,
		SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"},
		UpdatedAt:      staleUpdatedAt,
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil).Once()
	refreshedPr := pr
	refreshedPr.UpdatedAt = currentUpdatedAt
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(refreshedPr, nil).Once()

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, string(LenderStateManuallyClosed), *resultData.ActionResult.ToState)
	assert.Equal(t, LenderStateManuallyClosed, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.Equal(t, string(TerminateAction), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, currentUpdatedAt, mockPrRepo.savedPr.UpdatedAt)
	if assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Contains(t, *resultData.ActionResult.ChildActionError, "closing action cannot-supply failed")
		assert.Contains(t, *resultData.ActionResult.ChildActionError, "failed to create LMS adapter")
	}
	lmsCreator.AssertExpectations(t)
	mockPrRepo.AssertExpectations(t)
}

func TestHandleInvokeActionTerminateReportsReloadFailureBeforeFallback(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	action := TerminateAction
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:    patronRequestId,
		State: LenderStateValidated,
		Side:  SideLending,
	}, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, assert.AnError).Once()

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to reload patron request before local close", resultData.EventError.Message)
	mockPrRepo.AssertExpectations(t)
}

func TestHandleInvokeActionTerminateReportsUnavailableClosingActionOnFallback(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	action := TerminateAction
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:    patronRequestId,
		State: LenderStateValidated,
		Side:  SideLending,
	}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, string(LenderStateManuallyClosed), *resultData.ActionResult.ToState)
	if assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Equal(t, "closing action cannot-supply failed with status ERROR and outcome failure: LMS creator not configured", *resultData.ActionResult.ChildActionError)
	}
}

func TestHandleInvokeActionTerminateFallsBackWithoutClosingAction(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	action := TerminateAction
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:    patronRequestId,
		State: BorrowerStateSupplierLocated,
		Side:  SideBorrowing,
	}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, string(BorrowerStateManuallyClosed), *resultData.ActionResult.ToState)
	assert.Equal(t, BorrowerStateManuallyClosed, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.Equal(t, string(TerminateAction), mockPrRepo.savedPr.LastAction.String)
	assert.Nil(t, resultData.ActionResult.ChildActionError)
}

func TestHandleInvokeActionTerminateDeletesRequesterItem(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state pr_db.PatronRequestState
	}{
		{name: string(BorrowerStateReceived), state: BorrowerStateReceived},
		{name: string(BorrowerStateCheckedOut), state: BorrowerStateCheckedOut},
		{name: string(BorrowerStateCheckedIn), state: BorrowerStateCheckedIn},
		{name: "SHIPPED after partial receive", state: BorrowerStateShipped},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			lmsAdapter := new(mockLmsAdapter)
			lmsAdapter.On("DeleteItem", "item-1").Return(nil).Once()
			lmsCreator := new(MockLmsCreator)
			lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lmsAdapter, nil)
			prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
			pr := pr_db.PatronRequest{
				ID:              patronRequestId,
				State:           tt.state,
				Side:            SideBorrowing,
				RequesterSymbol: getDbText("ISIL:REC1"),
			}
			mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil).Once()
			mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
				{ID: "item-record-1", Barcode: "item-1", RequesterLmsItemCreated: true},
				{ID: "item-record-2", Barcode: "pre-existing-item", RequesterLmsItemCreated: false},
			}, nil).Once()
			action := TerminateAction

			status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

			assert.Equal(t, events.EventStatusSuccess, status)
			assert.Equal(t, string(BorrowerStateManuallyClosed), *resultData.ActionResult.ToState)
			assert.Nil(t, resultData.ActionResult.ChildActionError)
			assert.Equal(t, BorrowerStateManuallyClosed, mockPrRepo.savedPr.State)
			assert.True(t, mockPrRepo.savedPr.TerminalState)
			assert.Equal(t, string(TerminateAction), mockPrRepo.savedPr.LastAction.String)
			assert.False(t, mockPrRepo.requesterLmsItemCreated["item-record-1"])
			lmsAdapter.AssertNotCalled(t, "DeleteItem", "pre-existing-item")
			lmsAdapter.AssertExpectations(t)
		})
	}
}

func TestHandleInvokeActionTerminateReportsRequesterDeleteItemFailure(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(new(MockLmsAdapterFail), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateReceived,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REC1"),
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{ID: "item-record-1", Barcode: "item-1", RequesterLmsItemCreated: true}}, nil).Once()
	action := TerminateAction

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, string(BorrowerStateManuallyClosed), *resultData.ActionResult.ToState)
	if assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Contains(t, *resultData.ActionResult.ChildActionError, "requester item cleanup failed")
		assert.Contains(t, *resultData.ActionResult.ChildActionError, "LMS DeleteItem failed")
	}
	assert.Equal(t, BorrowerStateManuallyClosed, mockPrRepo.savedPr.State)
	assert.Equal(t, string(TerminateAction), mockPrRepo.savedPr.LastAction.String)
}

func TestHandleInvokeActionTerminateDoesNotDeleteUnconfirmedRequesterItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateShipped,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REC1"),
		LastAction:      getDbText(string(BorrowerActionReceive)),
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{
		ID:                      "item-record-1",
		Barcode:                 "pre-existing-item",
		RequesterLmsItemCreated: false,
	}}, nil).Once()
	action := TerminateAction

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, string(BorrowerStateManuallyClosed), *resultData.ActionResult.ToState)
	assert.Nil(t, resultData.ActionResult.ChildActionError)
	assert.Equal(t, BorrowerStateManuallyClosed, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionTerminateRejectsTerminal(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing requester symbol", resultData.EventError.Message)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeActionUpdateMetadataNeedReview(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)
	illRepo := new(IllRepoMock)

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:x"}, mock.Anything).Return([]ill_db.Peer{{Vendor: "other"}}, "", nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, illRepo, mockEventBus, new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	fakeEventID := "1234"
	pr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateValidated, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr, nil)

	mockEventBus.On("CreateNoticeWithParent", fakeEventID).Return("", nil)
	action := BorrowerActionUpdateMetadata

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{ID: fakeEventID, PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateNeedsReview, mockPrRepo.savedPr.State)
	assert.Equal(t, ActionOutcomeReview, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(BorrowerStateNeedsReview), *resultData.ActionResult.ToState)
}

func TestHandleInvokeActionUpdateMetadataMissingLookupParamsNeedsReview(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)
	illRepo := new(IllRepoMock)
	mode := dirapi.Merge

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:x"}, mock.Anything).Return([]ill_db.Peer{
		{
			Vendor: string(dirapi.CrossLink),
			CustomData: dirapi.Entry{
				Name:          "RS1",
				CatalogConfig: &dirapi.CatalogConfig{MetadataUpdateMode: &mode},
			},
		},
	}, "", nil)
	queryBuilder := catalog.NewQueryBuilderIsxn(false)
	lookupAdapter := catalog.CreateSruLookupAdapter(http.DefaultClient, []string{"http://unused"}, "", queryBuilder, nil, nil, "marcxml")
	lookupAdapterFactory := lookupFactoryWithAdapter(lookupAdapter)
	prAction := CreatePatronRequestActionService(mockPrRepo, illRepo, mockEventBus, new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), lookupAdapterFactory, nil)
	illRequest := iso18626.Request{}
	fakeEventID := "1234"
	pr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateValidated, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr, nil)

	action := BorrowerActionUpdateMetadata
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{ID: fakeEventID, PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, BorrowerStateNeedsReview, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, ActionOutcomeReview, mockPrRepo.savedPr.LastActionOutcome.String)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ToState) {
		assert.Equal(t, string(BorrowerStateNeedsReview), *resultData.ActionResult.ToState)
	}
	details, ok := resultData.CustomData["decisionDetails"].([]actionDecisionDetailMetadataUpdate)
	if assert.True(t, ok) && assert.Len(t, details, 1) {
		assert.Equal(t, "skipped", details[0].Outcome)
		assert.Equal(t, "missing-lookup-parameters", details[0].Reason)
		assert.Equal(t, dirapi.Merge, dirapi.MetadataUpdateMode(details[0].EffectiveMode))
	}
}

func TestHandleInvokeActionValidateSendRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)
	mockIso18626Handler := new(MockIso18626Handler)

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "12312"}}
	fakeEventID := "1234"
	//action := BorrowerActionUpdateMetadata
	initialPR := pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}
	validatedPR := initialPR
	validatedPR.State = BorrowerStateValidated
	updatePr := initialPR
	updatePr.State = BorrowerStateMetadataUpdated
	readyPr := updatePr
	readyPr.State = BorrowerStateReadyToSend
	sentPR := readyPr
	sentPR.State = BorrowerStateSent
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(sentPR, nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(updatePr, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(readyPr, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(sentPR, nil)
	mockEventBus.On("CreateNoticeWithParent", fakeEventID).Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{ID: fakeEventID, PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
	assert.Equal(t, string(BorrowerStateValidated), *resultData.ActionResult.ToState)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
}

func TestHandleInvokeActionValidateSendRequestDuplicateResponseIsFailure(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockIso18626Handler := new(MockIso18626Handler)
	mockEventBus := new(MockEventBus)
	patronRequestId := "duplicate"

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "12312"}}
	readyPr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateReadyToSend, Side: SideBorrowing}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(readyPr, nil)
	action := BorrowerActionSendRequest

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateReadyToSend, mockPrRepo.savedPr.State)
	assert.False(t, mockPrRepo.savedPr.TerminalState)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, string(BorrowerActionSendRequest), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Nil(t, resultData.OutgoingMessage)
	assert.Nil(t, resultData.IncomingMessage)
	if assert.Len(t, mockEventBus.createdNoticeData, 1) {
		assert.Equal(t, events.EventNameIllRequesterMessage, mockEventBus.createdNoticeNames[0])
		assert.Equal(t, events.EventStatusProblem, mockEventBus.createdNoticeStatus[0])
		assert.NotNil(t, mockEventBus.createdNoticeData[0].OutgoingMessage.Request)
		assert.Equal(t, iso18626.TypeMessageStatusERROR, mockEventBus.createdNoticeData[0].IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
	}
}

func TestHandleInvokeActionCloseRequestFromDuplicate(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      iso18626.Request{},
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"},
		State:           BorrowerStateDuplicate,
		Side:            SideBorrowing,
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	action := BorrowerActionCloseRequest

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		PatronRequestID: patronRequestId,
		EventData: events.EventData{CommonEventData: events.CommonEventData{
			Action: &action,
		}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, string(BorrowerStateClosedDuplicate), *resultData.ActionResult.ToState)
	assert.Equal(t, BorrowerStateClosedDuplicate, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.False(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, string(BorrowerActionCloseRequest), mockPrRepo.savedPr.LastAction.String)
}

func TestHandleInvokeActionTransitionActions(t *testing.T) {
	tests := []struct {
		name             string
		initialState     pr_db.PatronRequestState
		action           pr_db.PatronRequestAction
		expectedState    pr_db.PatronRequestState
		disableAutoState pr_db.PatronRequestState
		terminal         bool
	}{
		{
			name:             "skip patron validation from new",
			initialState:     BorrowerStateNew,
			action:           BorrowerActionSkipPatronValidation,
			expectedState:    BorrowerStateValidated,
			disableAutoState: BorrowerStateValidated,
		},
		{
			name:          "close new request locally",
			initialState:  BorrowerStateNew,
			action:        BorrowerActionCloseRequest,
			expectedState: BorrowerStateManuallyClosed,
			terminal:      true,
		},
		{
			name:             "skip patron validation",
			initialState:     BorrowerStateInvalidPatron,
			action:           BorrowerActionSkipPatronValidation,
			expectedState:    BorrowerStateValidated,
			disableAutoState: BorrowerStateValidated,
		},
		{
			name:          "close request locally",
			initialState:  BorrowerStateInvalidPatron,
			action:        BorrowerActionCloseRequest,
			expectedState: BorrowerStateManuallyClosed,
			terminal:      true,
		},
		{
			name:             "skip metadata update",
			initialState:     BorrowerStateValidated,
			action:           BorrowerActionSkipMetadataUpdate,
			expectedState:    BorrowerStateMetadataUpdated,
			disableAutoState: BorrowerStateMetadataUpdated,
		},
		{
			name:          "close patron validated request locally",
			initialState:  BorrowerStateValidated,
			action:        BorrowerActionCloseRequest,
			expectedState: BorrowerStateManuallyClosed,
			terminal:      true,
		},
		{
			name:          "close metadata updated request locally",
			initialState:  BorrowerStateMetadataUpdated,
			action:        BorrowerActionCloseRequest,
			expectedState: BorrowerStateManuallyClosed,
			terminal:      true,
		},
		{
			name:          "close ready to send request locally",
			initialState:  BorrowerStateReadyToSend,
			action:        BorrowerActionCloseRequest,
			expectedState: BorrowerStateManuallyClosed,
			terminal:      true,
		},
		{
			name:          "close request needing review locally",
			initialState:  BorrowerStateNeedsReview,
			action:        BorrowerActionCloseRequest,
			expectedState: BorrowerStateManuallyClosed,
			terminal:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), new(MockEventBus), new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)
			stateModel, err := LoadStateModelByName("default")
			assert.NoError(t, err)
			for stateIndex := range stateModel.States {
				state := &stateModel.States[stateIndex]
				if state.Name != string(tt.disableAutoState) || state.Actions == nil {
					continue
				}
				for actionIndex := range *state.Actions {
					(*state.Actions)[actionIndex].Trigger = nil
				}
			}
			prAction.actionMappingService.SMService = &StateModelService{stateMap: map[string]*proapi.StateModel{"default": stateModel}}
			pr := pr_db.PatronRequest{
				ID:             patronRequestId,
				IllRequest:     iso18626.Request{},
				State:          tt.initialState,
				Side:           SideBorrowing,
				NeedsAttention: true,
			}
			mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)

			status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
				PatronRequestID: patronRequestId,
				EventData: events.EventData{CommonEventData: events.CommonEventData{
					Action: &tt.action,
				}},
			})

			assert.Equal(t, events.EventStatusSuccess, status)
			assert.NotNil(t, resultData)
			assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
			assert.Equal(t, string(tt.expectedState), *resultData.ActionResult.ToState)
			assert.Equal(t, tt.expectedState, mockPrRepo.savedPr.State)
			assert.Equal(t, tt.terminal, mockPrRepo.savedPr.TerminalState)
			assert.False(t, mockPrRepo.savedPr.NeedsAttention)
			assert.Equal(t, string(tt.action), mockPrRepo.savedPr.LastAction.String)
		})
	}
}

func TestHandleInvokeActionSkipPatronValidationRunsConfiguredAutoActions(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)
	mockIso18626Handler := new(MockIso18626Handler)

	lmsCreator.On("GetAdapter", "ISIL:x").Return(createLmsAdapterMockLog(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "12312"}}
	fakeEventID := "1234"
	initialPR := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"},
		State:           BorrowerStateInvalidPatron,
		Side:            SideBorrowing,
		Tenant:          pgtype.Text{Valid: true, String: "testlib"},
		NeedsAttention:  true,
	}
	validatedPR := initialPR
	validatedPR.State = BorrowerStateValidated
	validatedPR.NeedsAttention = false
	metadataUpdatedPR := validatedPR
	metadataUpdatedPR.State = BorrowerStateMetadataUpdated
	readyPR := metadataUpdatedPR
	readyPR.State = BorrowerStateReadyToSend
	sentPR := readyPR
	sentPR.State = BorrowerStateSent
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(sentPR, nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(metadataUpdatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(readyPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(sentPR, nil)
	mockEventBus.On("CreateNoticeWithParent", fakeEventID).Return("", nil)
	action := BorrowerActionSkipPatronValidation

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              fakeEventID,
		PatronRequestID: patronRequestId,
		EventData: events.EventData{CommonEventData: events.CommonEventData{
			Action: &action,
		}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, string(BorrowerStateValidated), *resultData.ActionResult.ToState)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
	assert.Equal(t, string(BorrowerActionSendRequest), mockPrRepo.savedPr.LastAction.String)
}

func TestMetadataUpdateNoFactory(t *testing.T) {
	s := PatronRequestActionService{}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := s.metadataUpdate(ctx, &iso18626.Request{}, ill_db.Peer{})
	assert.NoError(t, err)
}

func TestMetadataUpdateAdapterInitError(t *testing.T) {
	creator := &mockLookupCreator{err: errors.New("adapter init failed")}
	factory := service.NewLookupAdapterFactory(nil, nil, "", nil, creator)
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := s.metadataUpdate(ctx, &iso18626.Request{}, ill_db.Peer{})
	assert.ErrorContains(t, err, "failed to get lookup adapter")
}

func TestMetadataUpdateNilLookupAdapter(t *testing.T) {
	creator := &mockLookupCreator{} // returns nil adapter, nil error
	factory := service.NewLookupAdapterFactory(nil, nil, "", nil, creator)
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := s.metadataUpdate(ctx, &iso18626.Request{}, ill_db.Peer{})
	assert.NoError(t, err)
}

func TestMetadataUpdateNoCatalogConfig(t *testing.T) {
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{})
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	peer := peerWithMetadataMode(nil) // CatalogConfig absent → mode stays None
	err := s.metadataUpdate(ctx, &iso18626.Request{}, peer)
	assert.NoError(t, err)
}

func TestMetadataUpdateModeNone(t *testing.T) {
	mode := dirapi.None
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{})
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := s.metadataUpdate(ctx, &iso18626.Request{}, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
}

func TestMetadataUpdateMetadataLookupError(t *testing.T) {
	mode := dirapi.Merge
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Err: errors.New("lookup failed")})
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	err := s.metadataUpdate(ctx, &iso18626.Request{}, peerWithMetadataMode(&mode))
	assert.ErrorContains(t, err, "failed to perform lookup for patron request")
}

func TestMetadataUpdateMergePopulatesEmptyFields(t *testing.T) {
	mode := dirapi.Merge
	meta := catalog.Metadata{Title: "Catalog Title", Author: "Jane Doe"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{} // empty bib info
	err := s.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Catalog Title", req.BibliographicInfo.Title)
	assert.Equal(t, "Jane Doe", req.BibliographicInfo.Author)
}

func TestMetadataUpdateMergePreservesExistingFields(t *testing.T) {
	mode := dirapi.Merge
	meta := catalog.Metadata{Title: "Catalog Title"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{Title: "Existing Title"},
	}
	err := s.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Existing Title", req.BibliographicInfo.Title) // not overwritten
}

func TestMetadataUpdateAutoModeWithIdentifierReplaces(t *testing.T) {
	mode := dirapi.Auto
	meta := catalog.Metadata{Title: "Catalog Title", Author: "Catalog Author", Isbn: "1234567890"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			Title:                  "Old Title",
			SupplierUniqueRecordId: "record-123", // non-empty → Auto resolves to Replace
			BibliographicItemId: []iso18626.BibliographicItemId{
				{
					BibliographicItemIdentifier:     "0987654321",
					BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ISBN"},
				},
			},
		},
	}
	err := s.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Catalog Title", req.BibliographicInfo.Title)                                              // replaced
	assert.Equal(t, "Catalog Author", req.BibliographicInfo.Author)                                            // replaced
	assert.Equal(t, "1234567890", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifier)    // replaced
	assert.Equal(t, "ISBN", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifierCode.Text) // replaced
}

func TestMetadataUpdateAutoModeWithoutIdentifierMerges(t *testing.T) {
	mode := dirapi.Auto
	meta := catalog.Metadata{Title: "Catalog Title", Author: "Catalog Author", Isbn: "1234567890", Issn: "4321-4321"}
	factory := lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: meta})
	s := PatronRequestActionService{
		lookupAdapterFactory: factory,
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{})
	req := &iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			Title: "Patron Title", // no SupplierUniqueRecordId → Auto resolves to Merge
			BibliographicItemId: []iso18626.BibliographicItemId{
				{
					BibliographicItemIdentifier:     "0987654321",
					BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ISBN"},
				},
			},
		},
	}
	err := s.metadataUpdate(ctx, req, peerWithMetadataMode(&mode))
	assert.NoError(t, err)
	assert.Equal(t, "Patron Title", req.BibliographicInfo.Title)                                               // preserved (Merge)
	assert.Equal(t, "Catalog Author", req.BibliographicInfo.Author)                                            // filled in (was empty)
	assert.Equal(t, "0987654321", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifier)    // kept
	assert.Equal(t, "ISBN", req.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifierCode.Text) // kept
	assert.Equal(t, "4321-4321", req.BibliographicInfo.BibliographicItemId[1].BibliographicItemIdentifier)     // added (not present)
	assert.Equal(t, "ISSN", req.BibliographicInfo.BibliographicItemId[1].BibliographicItemIdentifierCode.Text) // added (not present)
}

func TestHandleInvokeActionValidateGetAdapterFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:x").Return(lms.CreateLmsAdapterMockOK(), assert.AnError)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest, NeedsAttention: true}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
	assert.Equal(t, "assert.AnError general error for testing", resultData.EventError.Cause)
}

func TestHandleInvokeActionValidateLookupFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS LookupUser failed", resultData.EventError.Message)
	assert.Equal(t, BorrowerStateNew, mockPrRepo.savedPr.State)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(events.EventStatusError), mockPrRepo.savedPr.LastActionResult.String)
}

func TestHandleInvokeActionValidatePatronProblem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(&MockLmsAdapterPatronProblem{}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"},
		State:           BorrowerStateNew,
		Side:            SideBorrowing,
		Tenant:          pgtype.Text{Valid: true, String: "testlib"},
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, ActionOutcomeReview, resultData.ActionResult.Outcome)
	assert.Equal(t, string(BorrowerStateInvalidPatron), *resultData.ActionResult.ToState)
	assert.Equal(t, string(ncip.UnknownUser), resultData.Problem.Kind)
	assert.Equal(t, "patron was not found", resultData.Problem.Details)
	assert.Equal(t, BorrowerStateInvalidPatron, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, ActionOutcomeReview, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(events.EventStatusProblem), mockPrRepo.savedPr.LastActionResult.String)
}

func TestHandleInvokeActionRepeatedPatronProblemKeepsNeedsAttention(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(&MockLmsAdapterPatronProblem{}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      iso18626.Request{},
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"},
		State:           BorrowerStateInvalidPatron,
		Side:            SideBorrowing,
		NeedsAttention:  true,
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, ActionOutcomeReview, resultData.ActionResult.Outcome)
	if assert.NotNil(t, resultData.ActionResult.ToState) {
		assert.Equal(t, string(BorrowerStateInvalidPatron), *resultData.ActionResult.ToState)
	}
	assert.Equal(t, BorrowerStateInvalidPatron, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeActionSendRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateReadyToSend, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	action := BorrowerActionSendRequest
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData.IncomingMessage)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionReceiveOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("AcceptItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lmsAdapter, nil)
	mockEventBus := new(MockEventBus)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(false)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, emailMock, nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Once().Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateShipped, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
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
	assert.Nil(t, resultData.OutgoingMessage)
	assert.Nil(t, resultData.IncomingMessage)
	if assert.Len(t, mockEventBus.createdNoticeData, 1) {
		assert.Equal(t, events.EventNameIllRequesterMessage, mockEventBus.createdNoticeNames[0])
		assert.Equal(t, events.EventStatusSuccess, mockEventBus.createdNoticeStatus[0])
		assert.Equal(t, iso18626.TypeMessageStatusOK, mockEventBus.createdNoticeData[0].IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	}
	assert.Equal(t, BorrowerStateReceived, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.requesterLmsItemCreated["item1"])
	assert.True(t, mockPrRepo.requesterLmsItemCreated["item2"])
	lmsAdapter.AssertNumberOfCalls(t, "AcceptItem", 2)
	if assert.Len(t, lmsAdapter.Calls, 2) {
		assert.Equal(t, "AcceptItem", lmsAdapter.Calls[0].Method)
		assert.Equal(t, "1234", lmsAdapter.Calls[0].Arguments.String(0))
		assert.Equal(t, "AcceptItem", lmsAdapter.Calls[1].Method)
		assert.Equal(t, "5678", lmsAdapter.Calls[1].Arguments.String(0))
	}
}

func TestHandleInvokeActionReceivePersistsOnlyAcceptedItems(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("AcceptItem", "1234", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	lmsAdapter.On("AcceptItem", "5678", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("accept failed")).Once()
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateShipped,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REC1"),
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
		{ID: "item1", PrID: patronRequestId, Barcode: "1234"},
		{ID: "item2", PrID: patronRequestId, Barcode: "5678"},
	}, nil).Once()
	action := BorrowerActionReceive

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS AcceptItem failed", resultData.EventError.Message)
	assert.True(t, mockPrRepo.requesterLmsItemCreated["item1"])
	_, secondAccepted := mockPrRepo.requesterLmsItemCreated["item2"]
	assert.False(t, secondAccepted)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeActionReceiveSkipsPreviouslyAcceptedItems(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("AcceptItem", "5678", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	mockEventBus := new(MockEventBus)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(false)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, emailMock, nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateShipped,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REC1"),
		SupplierSymbol:  getDbText("ISIL:SUP1"),
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Once().Return(pr, nil)
	receivedPr := pr
	receivedPr.State = BorrowerStateReceived
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(receivedPr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
		{ID: "item1", PrID: patronRequestId, Barcode: "1234", RequesterLmsItemCreated: true},
		{ID: "item2", PrID: patronRequestId, Barcode: "5678"},
	}, nil).Once()
	action := BorrowerActionReceive

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, string(BorrowerStateReceived), *resultData.ActionResult.ToState)
	assert.True(t, mockPrRepo.requesterLmsItemCreated["item2"])
	lmsAdapter.AssertNotCalled(t, "AcceptItem", "1234", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeActionReceiveCompensatesWhenAcceptedItemCannotBeRecorded(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("AcceptItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil).Once()
	lmsAdapter.On("DeleteItem", "1234").Return(nil).Once()
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateShipped,
		Side:            SideBorrowing,
		RequesterSymbol: getDbText("ISIL:REC1"),
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{ID: "item1", PrID: patronRequestId, Barcode: "1234"}}, nil).Once()
	mockPrRepo.On("SetRequesterLmsItemCreated", pr_db.SetRequesterLmsItemCreatedParams{
		ID:                      "item1",
		RequesterLmsItemCreated: true,
	}).Return(errors.New("database unavailable")).Once()
	action := BorrowerActionReceive

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to record LMS AcceptItem", resultData.EventError.Message)
	lmsAdapter.AssertExpectations(t)
	mockPrRepo.AssertExpectations(t)
}

func TestHandleInvokeActionReceiveAcceptItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateCheckedIn, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionShipReturn
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData.IncomingMessage)
	assert.Equal(t, BorrowerStateShippedReturned, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionShipReturnItemFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	previousBrokerSymbol := configuredBrokerSymbol
	configuredBrokerSymbol = "ISIL:BROKER"
	t.Cleanup(func() { configuredBrokerSymbol = previousBrokerSymbol })

	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	// Leave the original request target empty to cover requests created before
	// broker-target normalization was introduced.
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: BorrowerStateWillSupply, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionCancelRequest
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData.IncomingMessage)
	if assert.NotNil(t, mockIso18626Handler.lastRequestingAgencyMessage) {
		assert.Equal(t, "ISIL", mockIso18626Handler.lastRequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text)
		assert.Equal(t, "BROKER", mockIso18626Handler.lastRequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
	}
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestCancelBorrowingRequestMissingRequesterSymbol(t *testing.T) {
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(new(MockPrRepo), new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, nil, new(EmailSenderMock), nil, nil)

	result := prAction.cancelBorrowingRequest(appCtx, "", pr_db.PatronRequest{})

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "missing requester symbol", result.result.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastRequestingAgencyMessage)
}

func TestCancelBorrowingRequestInvalidBrokerSymbol(t *testing.T) {
	previousBrokerSymbol := configuredBrokerSymbol
	configuredBrokerSymbol = "BROKER"
	t.Cleanup(func() { configuredBrokerSymbol = previousBrokerSymbol })

	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(new(MockPrRepo), new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, nil, new(EmailSenderMock), nil, nil)

	result := prAction.cancelBorrowingRequest(appCtx, "", pr_db.PatronRequest{
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"},
	})

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid supplier symbol", result.result.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastRequestingAgencyMessage)
}

func TestHandleInvokeActionAcceptCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionAcceptCondition
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData.IncomingMessage)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateConditionPending, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	action := BorrowerActionRejectCondition
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData.IncomingMessage)
	if assert.NotNil(t, mockIso18626Handler.lastRequestingAgencyMessage) {
		assert.Equal(t, iso18626.TypeActionCancel, mockIso18626Handler.lastRequestingAgencyMessage.Action)
		assert.Equal(t, shim.RESHARE_LOAN_CONDITION_REJECT, mockIso18626Handler.lastRequestingAgencyMessage.Note)
		assert.Equal(t, "SUP1", mockIso18626Handler.lastRequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
		assert.False(t, mockIso18626Handler.lastRequestingAgencyMessage.Header.Timestamp.IsZero())
	}
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionRejectConditionMarksReceivedConditionNotificationsRejected(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, nil, new(EmailSenderMock), nil, nil)
	var request iso18626.Request
	status, result, err := prAction.messageSender.sendBorrowingRequest(appCtx, "", pr_db.PatronRequest{State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}}, request)

	assert.Equal(t, events.EventStatusError, status)
	assert.Nil(t, result)
	assert.EqualError(t, err, "invalid requester symbol")
}

func TestSendBorrowingRequestZeroValueIllRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, nil, new(EmailSenderMock), nil, nil)

	status, _, err := prAction.messageSender.sendBorrowingRequest(appCtx, "", pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateValidated,
		Side:            SideBorrowing,
		Patron:          pgtype.Text{Valid: true, String: "patron1"},
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"},
	}, iso18626.Request{})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NoError(t, err)
	if assert.Len(t, mockEventBus.createdNoticeData, 1) &&
		assert.NotNil(t, mockEventBus.createdNoticeData[0].OutgoingMessage) &&
		assert.NotNil(t, mockEventBus.createdNoticeData[0].OutgoingMessage.Request) {
		request := mockEventBus.createdNoticeData[0].OutgoingMessage.Request
		assert.Equal(t, "ISIL", request.Header.RequestingAgencyId.AgencyIdType.Text)
		assert.Equal(t, "REC1", request.Header.RequestingAgencyId.AgencyIdValue)
		assert.Equal(t, patronRequestId, request.Header.RequestingAgencyRequestId)
		assert.False(t, request.Header.Timestamp.IsZero())
		assert.Equal(t, "patron1", request.PatronInfo.PatronId)
		assert.Equal(t, iso18626.BibliographicInfo{}, request.BibliographicInfo)
	}
	assert.Equal(t, iso18626.TypeMessageStatusOK, mockEventBus.createdNoticeData[0].IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
}

func TestSendBorrowingRequestPreservesIllRequestFields(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, nil, new(EmailSenderMock), nil, nil)

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

	status, _, err := prAction.messageSender.sendBorrowingRequest(appCtx, "", pr_db.PatronRequest{
		ID:              patronRequestId,
		State:           BorrowerStateValidated,
		Side:            SideBorrowing,
		Patron:          pgtype.Text{Valid: true, String: "patron1"},
		RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"},
	}, illRequest)

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NoError(t, err)
	if assert.Len(t, mockEventBus.createdNoticeData, 1) &&
		assert.NotNil(t, mockEventBus.createdNoticeData[0].OutgoingMessage) &&
		assert.NotNil(t, mockEventBus.createdNoticeData[0].OutgoingMessage.Request) {
		request := mockEventBus.createdNoticeData[0].OutgoingMessage.Request
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
	assert.Equal(t, iso18626.TypeMessageStatusOK, mockEventBus.createdNoticeData[0].IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
}

func TestShipReturnBorrowingRequestMissingSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	illRequest := iso18626.Request{}
	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, "", pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateValidated, Side: SideBorrowing, Patron: pgtype.Text{Valid: true, String: "patron1"}, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "missing supplier symbol", result.result.EventError.Message)
}

func TestShipReturnBorrowingRequestMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, "", pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateValidated, Side: SideBorrowing, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "missing requester symbol", result.result.EventError.Message)
}

func TestShipReturnBorrowingRequestInvalidSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "ISIL:REC1"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, "", pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}, SupplierSymbol: pgtype.Text{Valid: true, String: "x"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid supplier symbol", result.result.EventError.Message)
}

func TestShipReturnBorrowingRequestInvalidRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := lms.CreateLmsAdapterMockOK()
	lmsCreator.On("GetAdapter", pgtype.Text{Valid: true, String: "x"}).Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	var request iso18626.Request
	result := prAction.shipReturnBorrowingRequest(appCtx, "", pr_db.PatronRequest{ID: patronRequestId, State: BorrowerStateValidated, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "x"}, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, lmsAdapter, request)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "invalid requester symbol", result.result.EventError.Message)
}

func TestHandleInvokeLenderActionNoSupplierSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateNew, Side: SideLending}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing supplier symbol", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionNoLms(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), assert.AnError)

	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateNew, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to create LMS adapter", resultData.EventError.Message)
}

func TestHandleInvokeLenderActionValidatePatron(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}

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
	willSupplyPendingPR := validatedPR
	willSupplyPendingPR.State = LenderStateWillSupplyPending
	willSupplyPR := validatedPR
	willSupplyPR.State = LenderStateWillSupply

	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPendingPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPR, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	mockEventBus.On("CreateNoticeWithParent", "invoke-validate").Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-validate",
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron, User: "okapi-user-1"}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.Len(t, mockEventBus.createdTaskData, 2)
	assert.NotNil(t, mockEventBus.createdTaskData[0].Action)
	assert.Equal(t, LenderActionRequestItem, *mockEventBus.createdTaskData[0].Action)
	assert.NotNil(t, mockEventBus.createdTaskData[1].Action)
	assert.Equal(t, LenderActionWillSupply, *mockEventBus.createdTaskData[1].Action)
	assert.Equal(t, "okapi-user-1", mockEventBus.createdTaskData[0].User)
	assert.Equal(t, "okapi-user-1", mockEventBus.createdTaskData[1].User)
}

func TestHandleInvokeLenderActionValidateAutoActionError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Equal(t, "auto action request-item failed with status ERROR: failed to read patron request", *resultData.ActionResult.ChildActionError)
	}
	assert.True(t, mockPrRepo.savedPr.LastAction.Valid)
	assert.Equal(t, string(LenderActionRequestItem), mockPrRepo.savedPr.LastAction.String)
	assert.True(t, mockPrRepo.savedPr.LastActionOutcome.Valid)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.True(t, mockPrRepo.savedPr.LastActionResult.Valid)
	assert.Equal(t, string(events.EventStatusError), mockPrRepo.savedPr.LastActionResult.String)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeLenderActionValidateHandlesRequestItemFailureTransition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := &MockLmsAdapterLog{requestItemErr: errors.New("RequestItem failed")}
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}

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
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	mockEventBus.On("CreateNoticeWithParent", "invoke-validate").Return("", nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-validate",
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) {
		assert.Nil(t, resultData.ActionResult.ChildActionError)
	}
	assert.Equal(t, LenderStateItemPending, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, string(LenderActionRequestItem), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(events.EventStatusError), mockPrRepo.savedPr.LastActionResult.String)
}

func TestHandleInvokeLenderActionValidateHandlesWillSupplyFailureTransition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", "req-1", "", "", "", "").Return(&lms.RequestedItem{Barcode: "item-1"}, nil).Once()
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := &MockIso18626Handler{failSupplyingAgencyMessage: true}
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:      iso18626.Header{RequestingAgencyRequestId: "req-1"},
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}
	initialPR := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           LenderStateNew,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		RequesterReqID:  getDbText("req-1"),
	}
	validatedPR := initialPR
	validatedPR.State = LenderStateValidated
	willSupplyPendingPR := validatedPR
	willSupplyPendingPR.State = LenderStateWillSupplyPending
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(initialPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPendingPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPendingPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPendingPR, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	action := LenderActionValidatePatron

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-validate",
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) {
		assert.Nil(t, resultData.ActionResult.ChildActionError)
	}
	assert.Equal(t, LenderStateWillSupplyPending, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, string(LenderActionWillSupply), mockPrRepo.savedPr.LastAction.String)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.Equal(t, string(events.EventStatusProblem), mockPrRepo.savedPr.LastActionResult.String)
	assert.Len(t, mockPrRepo.savedItems, 1)
	assert.Len(t, mockEventBus.createdTaskData, 2)
	assert.Equal(t, LenderActionRequestItem, *mockEventBus.createdTaskData[0].Action)
	assert.Equal(t, LenderActionWillSupply, *mockEventBus.createdTaskData[1].Action)
	lmsAdapter.AssertExpectations(t)
}

func TestRunAutoActionsHandlesPersistedFailureTransition(t *testing.T) {
	mockEventBus := new(MockEventBus)
	toState := string(LenderStateItemPending)
	mockEventBus.On("ProcessExclusiveTask", patronRequestId+"-task-1").Return(events.Event{
		EventStatus: events.EventStatusError,
		ResultData: events.EventResult{CommonEventData: events.CommonEventData{
			ActionResult: &events.ActionResult{
				Outcome: ActionOutcomeFailure,
				ToState: &toState,
			},
		}},
	}, nil)
	prAction := CreatePatronRequestActionService(new(MockPrRepo), new(IllRepoMock), mockEventBus, new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)

	err := prAction.RunAutoActionsOnStateEntry(appCtx, pr_db.PatronRequest{
		ID:    patronRequestId,
		State: LenderStateValidated,
		Side:  SideLending,
		IllRequest: iso18626.Request{
			ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
		},
	}, nil, "")

	assert.NoError(t, err)
}

func TestRunAutoActionsPropagatesUnboundFailure(t *testing.T) {
	mockEventBus := new(MockEventBus)
	mockEventBus.On("ProcessExclusiveTask", patronRequestId+"-task-1").Return(events.Event{
		EventStatus: events.EventStatusError,
		ResultData: events.EventResult{CommonEventData: events.CommonEventData{
			ActionResult: &events.ActionResult{Outcome: ActionOutcomeFailure},
			EventError:   &events.EventError{Message: "action failed"},
		}},
	}, nil)
	prAction := CreatePatronRequestActionService(new(MockPrRepo), new(IllRepoMock), mockEventBus, new(MockIso18626Handler), nil, new(EmailSenderMock), nil, nil)

	err := prAction.RunAutoActionsOnStateEntry(appCtx, pr_db.PatronRequest{
		ID:    patronRequestId,
		State: BorrowerStateNew,
		Side:  SideBorrowing,
		IllRequest: iso18626.Request{
			ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
		},
	}, nil, "")

	assert.EqualError(t, err, "auto action validate-patron failed with status ERROR: action failed")
}

func TestHandleInvokeLenderActionValidateAutoActionCreateTaskError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.createTaskErr = errors.New("event bus error")
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &actionValidatePatron}},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ChildActionError) {
		assert.Equal(t, "event bus error", *resultData.ActionResult.ChildActionError)
	}
	assert.True(t, mockPrRepo.savedPr.LastAction.Valid)
	assert.Equal(t, string(LenderActionRequestItem), mockPrRepo.savedPr.LastAction.String)
	assert.True(t, mockPrRepo.savedPr.LastActionOutcome.Valid)
	assert.Equal(t, ActionOutcomeFailure, mockPrRepo.savedPr.LastActionOutcome.String)
	assert.True(t, mockPrRepo.savedPr.LastActionResult.Valid)
	assert.Equal(t, string(events.EventStatusError), mockPrRepo.savedPr.LastActionResult.String)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestRequestItemLenderRequestUsesIllTitleWhenResponseTitleIsEmpty(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&lms.RequestedItem{Barcode: "1", CallNumber: "2"}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}, BibliographicInfo: iso18626.BibliographicInfo{Title: "title1"}}
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	result := prAction.requestItemLenderRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: getDbText("ISIL:REQ1")}, lmsAdapter, illRequest)

	assert.Equal(t, events.EventStatusSuccess, result.status)
	assert.Len(t, mockPrRepo.savedItems, 1)
	assert.Equal(t, "1", mockPrRepo.savedItems[0].Barcode)
	assert.Equal(t, "2", mockPrRepo.savedItems[0].CallNumber.String)
	assert.Equal(t, "title1", mockPrRepo.savedItems[0].Title.String)
	lmsAdapter.AssertNumberOfCalls(t, "RequestItem", 1)
}

func TestHandleInvokeLenderActionWillSupplyDoesNotRequestItem(t *testing.T) {
	tests := []struct {
		serviceType iso18626.TypeServiceType
		state       pr_db.PatronRequestState
	}{
		{serviceType: iso18626.TypeServiceTypeCopy, state: LenderStateValidated},
		{serviceType: iso18626.TypeServiceTypeCopyOrLoan, state: LenderStateValidated},
		{serviceType: iso18626.TypeServiceTypeLoan, state: LenderStateWillSupplyPending},
	}
	for _, test := range tests {
		serviceType := test.serviceType
		t.Run(string(serviceType), func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			lmsCreator := new(MockLmsCreator)
			lmsAdapter := new(mockLmsAdapter)
			lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
			mockIso18626Handler := new(MockIso18626Handler)
			prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
			illRequest := iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: serviceType}}
			mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: test.state, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
			action := LenderActionWillSupply

			status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

			assert.Equal(t, events.EventStatusSuccess, status)
			assert.NotNil(t, resultData)
			assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
			assert.Empty(t, mockPrRepo.savedItems)
			lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}

func TestRequestItemLenderRequestUsesResponseTitleWhenAvailable(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(&lms.RequestedItem{RequestID: "lms-req-1", Barcode: "1", CallNumber: "2", Title: "title2"}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}, BibliographicInfo: iso18626.BibliographicInfo{Title: "title1"}}
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	result := prAction.requestItemLenderRequest(appCtx, pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: getDbText("ISIL:REQ1")}, lmsAdapter, illRequest)

	assert.Equal(t, events.EventStatusSuccess, result.status)
	assert.Len(t, mockPrRepo.savedItems, 1)
	assert.Equal(t, "1", mockPrRepo.savedItems[0].Barcode)
	assert.Equal(t, "2", mockPrRepo.savedItems[0].CallNumber.String)
	assert.Equal(t, "title2", mockPrRepo.savedItems[0].Title.String)
	assert.Equal(t, "lms-req-1", mockPrRepo.savedItems[0].LmsRequestID.String)
	lmsAdapter.AssertNumberOfCalls(t, "RequestItem", 1)
}

func TestRequestItemLenderRequestResumesAfterSavedItem(t *testing.T) {
	savedItems := []pr_db.Item{{PrID: patronRequestId, Barcode: "item-1", LmsRequestID: getDbText("lms-req-1")}}
	mockPrRepo := &MockPrRepo{savedItems: savedItems}
	lmsAdapter := new(mockLmsAdapter)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		NeedsAttention:  true,
	}
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return(savedItems, nil).Once()
	result := prAction.requestItemLenderRequest(appCtx, pr, lmsAdapter, illRequest)

	assert.Equal(t, events.EventStatusSuccess, result.status)
	assert.Len(t, mockPrRepo.savedItems, 1)
	lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestRequestItemLenderRequestExistingItemLookupFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsAdapter := new(mockLmsAdapter)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
	}
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, assert.AnError)
	result := prAction.requestItemLenderRequest(appCtx, pr, lmsAdapter, illRequest)

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "failed to get existing items", result.result.EventError.Message)
	lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	mockPrRepo.AssertExpectations(t)
}

func TestHandleInvokeLenderActionRejectCancel(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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

func TestHandleInvokeLenderActionRequestItemNcipFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(createLmsAdapterMockFail(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)

	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	action := LenderActionRequestItem
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS RequestItem failed", resultData.EventError.Message)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ToState) {
		assert.Equal(t, string(LenderStateItemPending), *resultData.ActionResult.ToState)
	}
	assert.Equal(t, LenderStateItemPending, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
}

func TestHandleInvokeLenderActionRequestItemSaveItemFailed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", "req-1", "", "", "", "").Return(&lms.RequestedItem{RequestID: "lms-req-1", Barcode: "item-1"}, nil)
	lmsAdapter.On("CancelRequestItem", "lms-req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	mockPrRepo.saveItemFail = true
	mockPrRepo.On("GetPatronRequestByIdForUpdate", patronRequestId).Return(pr_db.PatronRequest{RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:x"}, State: BorrowerStateNew, Side: SideBorrowing, Tenant: pgtype.Text{Valid: true, String: "testlib"}, IllRequest: illRequest}, nil)
	action := LenderActionRequestItem
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to save item", resultData.EventError.Message)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionRequestItemRejectsEmptyBarcode(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", "req-1", "", "", "", "").Return(&lms.RequestedItem{RequestID: "lms-req-1"}, nil)
	lmsAdapter.On("CancelRequestItem", "lms-req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}, ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	action := LenderActionRequestItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS RequestItem returned an empty barcode", resultData.EventError.Message)
	assert.Empty(t, mockPrRepo.savedItems)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionRequestItemWhenNcipIsDisabled(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:      iso18626.Header{RequestingAgencyRequestId: "req-1"},
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}
	validatedPR := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           LenderStateValidated,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
	}
	willSupplyPendingPR := validatedPR
	willSupplyPendingPR.State = LenderStateWillSupplyPending
	willSupplyPR := validatedPR
	willSupplyPR.State = LenderStateWillSupply
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(validatedPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPendingPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPR, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	action := LenderActionRequestItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	assert.Empty(t, mockPrRepo.savedItems)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Len(t, mockEventBus.createdTaskData, 1)
	assert.Equal(t, LenderActionWillSupply, *mockEventBus.createdTaskData[0].Action)
}

func TestCancelLenderRequestItemsNoSavedItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	lmsAdapter := new(mockLmsAdapter)

	err := prAction.cancelLenderRequestItems(
		appCtx,
		pr_db.PatronRequest{ID: patronRequestId},
		lmsAdapter,
	)

	assert.NoError(t, err)
	lmsAdapter.AssertNotCalled(t, "CancelRequestItem", mock.Anything, mock.Anything)
}

func TestCancelLenderRequestItemsManualItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1"}}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	lmsAdapter := new(mockLmsAdapter)

	err := prAction.cancelLenderRequestItems(
		appCtx,
		pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: getDbText("ISIL:REQ1")},
		lmsAdapter,
	)

	assert.NoError(t, err)
	lmsAdapter.AssertNotCalled(t, "CancelRequestItem", mock.Anything, mock.Anything)
}

func TestCancelLenderRequestItemsInvalidRequesterSymbol(t *testing.T) {
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
			mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
			prAction := &PatronRequestActionService{prRepo: mockPrRepo}
			lmsAdapter := new(mockLmsAdapter)

			err := prAction.cancelLenderRequestItems(
				appCtx,
				pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: tt.symbol},
				lmsAdapter,
			)

			assert.EqualError(t, err, "invalid requester symbol")
			lmsAdapter.AssertNotCalled(t, "CancelRequestItem", mock.Anything, mock.Anything)
		})
	}
}

func TestCancelLenderRequestItemsCancelsEachDistinctLmsRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
		{Barcode: "item-1", LmsRequestID: getDbText("lms-request-1")},
		{Barcode: "item-2", LmsRequestID: getDbText("lms-request-2")},
		{Barcode: "item-3", LmsRequestID: getDbText("lms-request-1")},
		{Barcode: "manual-item"},
	}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "lms-request-1", "").Return(nil).Once()
	lmsAdapter.On("CancelRequestItem", "lms-request-2", "").Return(nil).Once()

	err := prAction.cancelLenderRequestItems(
		appCtx,
		pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: getDbText("ISIL:REQ1")},
		lmsAdapter,
	)

	assert.NoError(t, err)
	lmsAdapter.AssertExpectations(t)
}

func TestCancelLenderRequestItemsAttemptsAllRequestsWhenCancellationFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{
		{Barcode: "item-1", LmsRequestID: getDbText("lms-request-1")},
		{Barcode: "item-2", LmsRequestID: getDbText("lms-request-2")},
	}, nil)
	prAction := &PatronRequestActionService{prRepo: mockPrRepo}
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "lms-request-1", "").Return(errors.New("first cancellation failed")).Once()
	lmsAdapter.On("CancelRequestItem", "lms-request-2", "").Return(errors.New("second cancellation failed")).Once()

	err := prAction.cancelLenderRequestItems(
		appCtx,
		pr_db.PatronRequest{ID: patronRequestId, RequesterSymbol: getDbText("ISIL:REQ1")},
		lmsAdapter,
	)

	assert.ErrorContains(t, err, "cancel LMS request lms-request-1: first cancellation failed")
	assert.ErrorContains(t, err, "cancel LMS request lms-request-2: second cancellation failed")
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionCannotSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	mockEventBus := new(MockEventBus)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
	action := LenderActionCannotSupply
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Nil(t, resultData.OutgoingMessage)
	assert.Nil(t, resultData.IncomingMessage)
	assert.Equal(t, LenderStateUnfilled, mockPrRepo.savedPr.State)
	lmsAdapter.AssertExpectations(t)
	if assert.Len(t, mockEventBus.createdNoticeData, 1) {
		assert.Equal(t, events.EventStatusSuccess, mockEventBus.createdNoticeStatus[0])
		assert.NotNil(t, mockEventBus.createdNoticeData[0].OutgoingMessage.SupplyingAgencyMessage)
		assert.NotNil(t, mockEventBus.createdNoticeData[0].IncomingMessage.SupplyingAgencyMessageConfirmation)
	}

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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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

func TestHandleInvokeLenderActionCannotSupplyContinuesWhenCancelRequestItemFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(errors.New("cancel failed"))
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
	action := LenderActionCannotSupply

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, LenderStateUnfilled, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.False(t, mockPrRepo.savedPr.NeedsAttention)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	compensation, ok := resultData.CustomData[compensationResultKey].(actionCompensationResult)
	if assert.True(t, ok) {
		assert.Equal(t, "CancelRequestItem", compensation.Operation)
		assert.Equal(t, ActionOutcomeFailure, compensation.Outcome)
		assert.Contains(t, compensation.Error, "cancel failed")
	}
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionCannotSupplyIsoFailureStillBlocksTransition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(errors.New("cancel failed"))
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := &MockIso18626Handler{failSupplyingAgencyMessage: true}
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
	action := LenderActionCannotSupply

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
	}})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	assert.False(t, mockPrRepo.savedPr.TerminalState)
	compensation, ok := resultData.CustomData[compensationResultKey].(actionCompensationResult)
	if assert.True(t, ok) {
		assert.Contains(t, compensation.Error, "cancel failed")
	}
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionAddItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(&lms.LmsAdapterManual{}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      iso18626.Request{BibliographicInfo: iso18626.BibliographicInfo{Title: "request title"}, ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}},
		State:           LenderStateWillSupply,
		Side:            SideLending,
		RequesterSymbol: getDbText("ISIL:REQ1"),
		SupplierSymbol:  getDbText("ISIL:SUP1"),
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "existing"}}, nil)
	action := LenderActionAddItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"barcode":    "  scanned-barcode  ",
			"callNumber": "  QA 123  ",
			"itemId":     "  item-2  ",
		},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	if assert.Len(t, mockPrRepo.savedItems, 1) {
		item := mockPrRepo.savedItems[0]
		assert.Equal(t, "scanned-barcode", item.Barcode)
		assert.Equal(t, "QA 123", item.CallNumber.String)
		assert.Equal(t, "request title", item.Title.String)
		assert.Equal(t, "item-2", item.ItemID.String)
		assert.False(t, item.LmsRequestID.Valid)
	}
}

func TestHandleInvokeLenderActionAddItemContinuesFromItemPending(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.runTaskHandler = true
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:      iso18626.Header{RequestingAgencyRequestId: "req-1"},
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}
	itemPendingPR := pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           LenderStateItemPending,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
	}
	willSupplyPendingPR := itemPendingPR
	willSupplyPendingPR.State = LenderStateWillSupplyPending
	willSupplyPR := itemPendingPR
	willSupplyPR.State = LenderStateWillSupply
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(itemPendingPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPendingPR, nil).Once()
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(willSupplyPR, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	action := LenderActionAddItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		PatronRequestID: patronRequestId,
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{Action: &action},
			CustomData:      map[string]any{"barcode": "manual-item"},
		},
	})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, LenderStateWillSupply, mockPrRepo.savedPr.State)
	if assert.Len(t, mockPrRepo.savedItems, 1) {
		assert.Equal(t, "manual-item", mockPrRepo.savedItems[0].Barcode)
		assert.False(t, mockPrRepo.savedItems[0].LmsRequestID.Valid)
	}
	lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
	assert.Len(t, mockEventBus.createdTaskData, 1)
	assert.Equal(t, LenderActionWillSupply, *mockEventBus.createdTaskData[0].Action)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
}

func TestHandleInvokeLenderActionAddItemRejectsMissingOrDuplicateBarcode(t *testing.T) {
	tests := []struct {
		name        string
		barcode     string
		items       []pr_db.Item
		expectRead  bool
		expectError string
	}{
		{name: "missing", barcode: "  ", expectError: "barcode is required"},
		{name: "duplicate", barcode: " barcode-1 ", items: []pr_db.Item{{Barcode: "barcode-1"}}, expectRead: true, expectError: "item barcode is already attached to the request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			lmsCreator := new(MockLmsCreator)
			lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(new(mockLmsAdapter), nil)
			prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
			pr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}}, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1")}
			mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
			if tt.expectRead {
				mockPrRepo.On("GetItemsByPrId", patronRequestId).Return(tt.items, nil)
			}
			action := LenderActionAddItem

			status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
				CommonEventData: events.CommonEventData{Action: &action},
				CustomData:      map[string]any{"barcode": tt.barcode},
			}})

			assert.Equal(t, events.EventStatusError, status)
			assert.Equal(t, tt.expectError, resultData.EventError.Message)
			assert.Empty(t, mockPrRepo.savedItems)
		})
	}
}

func TestHandleInvokeLenderActionRemoveManualItem(t *testing.T) {
	item := pr_db.Item{ID: "item-record-1", PrID: patronRequestId, Barcode: "barcode-1"}
	mockPrRepo := &MockPrRepo{savedItems: []pr_db.Item{item}}
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(&lms.LmsAdapterManual{}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}}, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1")}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{item}, nil)
	action := LenderActionRemoveItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData:      map[string]any{"barcode": " barcode-1 "},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, []string{"item-record-1"}, mockPrRepo.deletedItemIDs)
	assert.Empty(t, mockPrRepo.savedItems)
}

func TestHandleInvokeLenderActionRemoveItemKeepsItemPendingAttention(t *testing.T) {
	item := pr_db.Item{ID: "item-record-1", PrID: patronRequestId, Barcode: "barcode-1"}
	mockPrRepo := &MockPrRepo{savedItems: []pr_db.Item{item}}
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(&lms.LmsAdapterManual{}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{
		ID:             patronRequestId,
		IllRequest:     iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}},
		State:          LenderStateItemPending,
		Side:           SideLending,
		SupplierSymbol: getDbText("ISIL:SUP1"),
		NeedsAttention: true,
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{item}, nil)
	action := LenderActionRemoveItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData:      map[string]any{"barcode": "barcode-1"},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	if assert.NotNil(t, resultData.ActionResult.ToState) {
		assert.Equal(t, string(LenderStateItemPending), *resultData.ActionResult.ToState)
	}
	assert.Equal(t, LenderStateItemPending, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Empty(t, mockPrRepo.savedItems)
}

func TestHandleInvokeLenderActionRemoveLmsRequestedItem(t *testing.T) {
	item := pr_db.Item{ID: "item-record-1", PrID: patronRequestId, Barcode: "barcode-1", LmsRequestID: getDbText("lms-request-1")}
	mockPrRepo := &MockPrRepo{savedItems: []pr_db.Item{item}}
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "lms-request-1", "").Return(nil).Once()
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}}, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{item}, nil)
	action := LenderActionRemoveItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData:      map[string]any{"barcode": "barcode-1"},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, []string{"item-record-1"}, mockPrRepo.deletedItemIDs)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionRemoveItemKeepsItemWhenLmsCancelFails(t *testing.T) {
	item := pr_db.Item{ID: "item-record-1", PrID: patronRequestId, Barcode: "barcode-1", LmsRequestID: getDbText("lms-request-1")}
	mockPrRepo := &MockPrRepo{savedItems: []pr_db.Item{item}}
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "lms-request-1", "").Return(errors.New("cancel failed")).Once()
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}}, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{item}, nil)
	action := LenderActionRemoveItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData:      map[string]any{"barcode": "barcode-1"},
	}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS CancelRequestItem failed", resultData.EventError.Message)
	assert.Empty(t, mockPrRepo.deletedItemIDs)
	assert.Len(t, mockPrRepo.savedItems, 1)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionRemoveItemRetainsItemWhenDeleteFailsAfterCancel(t *testing.T) {
	item := pr_db.Item{ID: "item-record-1", PrID: patronRequestId, Barcode: "barcode-1", LmsRequestID: getDbText("lms-request-1")}
	mockPrRepo := &MockPrRepo{savedItems: []pr_db.Item{item}, deleteItemFail: true}
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "lms-request-1", "").Return(nil).Once()
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	pr := pr_db.PatronRequest{ID: patronRequestId, IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}}, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{item}, nil)
	action := LenderActionRemoveItem

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData:      map[string]any{"barcode": "barcode-1"},
	}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to delete item", resultData.EventError.Message)
	assert.Empty(t, mockPrRepo.deletedItemIDs)
	assert.Len(t, mockPrRepo.savedItems, 1)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionAddConditionOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:            iso18626.Header{RequestingAgencyRequestId: "req-1"},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "bib-1"},
		ServiceInfo:       &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupplyPending, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
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
	assert.Empty(t, mockPrRepo.savedItems)
	lmsAdapter.AssertExpectations(t)

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

func TestHandleInvokeLenderActionAddConditionLogsIsoExchangeWhenSavingNotificationFails(t *testing.T) {
	mockPrRepo := &MockPrRepo{saveNotificationFail: true}
	mockEventBus := new(MockEventBus)
	mockIso18626Handler := new(MockIso18626Handler)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:      iso18626.Header{RequestingAgencyRequestId: "req-1"},
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupplyPending, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		ID:              "invoke-add-condition",
		PatronRequestID: patronRequestId,
		EventData: events.EventData{
			CommonEventData: events.CommonEventData{Action: &action},
			CustomData:      map[string]any{"loanCondition": "my condition"},
		},
	})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to save add-condition notification", resultData.EventError.Message)
	assert.Nil(t, resultData.OutgoingMessage)
	assert.Nil(t, resultData.IncomingMessage)
	if assert.Len(t, mockEventBus.createdNoticeData, 1) {
		assert.Equal(t, events.EventStatusSuccess, mockEventBus.createdNoticeStatus[0])
		assert.Equal(t, "invoke-add-condition", mockEventBus.createdNoticeParent[0])
		assert.NotNil(t, mockEventBus.createdNoticeData[0].OutgoingMessage.SupplyingAgencyMessage)
		assert.NotNil(t, mockEventBus.createdNoticeData[0].IncomingMessage.SupplyingAgencyMessageConfirmation)
	}
}

func TestHandleInvokeLenderActionAddConditionDoesNotRequestItem(t *testing.T) {
	item := pr_db.Item{ID: "item-record-1", PrID: patronRequestId, Barcode: "item-1", LmsRequestID: getDbText("req-1")}
	mockPrRepo := &MockPrRepo{savedItems: []pr_db.Item{item}}
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:      iso18626.Header{RequestingAgencyRequestId: "req-1"},
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData:      map[string]any{"loanCondition": "my condition"},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, ActionOutcomeSuccess, resultData.ActionResult.Outcome)
	assert.Equal(t, LenderStateConditionPending, mockPrRepo.savedPr.State)
	assert.Len(t, mockPrRepo.savedItems, 1)
	lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestHandleInvokeLenderActionAskRetryMissingItemId(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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

func TestHandleInvokeLenderActionAskRetryInvalidRequesterDoesNotPanic(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), new(MockIso18626Handler), lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           LenderStateValidated,
		Side:            SideLending,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: pgtype.Text{},
	}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
	action := LenderActionAskRetry

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"itemId":      "0201896834",
			"reasonRetry": string(iso18626.ReasonRetryNotFoundAsCited),
		},
	}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "invalid requester symbol", resultData.EventError.Message)
	assert.Equal(t, LenderStateValidated, mockPrRepo.savedPr.State)
	compensation, ok := resultData.CustomData[compensationResultKey].(actionCompensationResult)
	if assert.True(t, ok) {
		assert.Contains(t, compensation.Error, "invalid requester symbol")
	}
}

func TestHandleInvokeLenderActionAskRetryFull(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
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

func TestHandleInvokeLenderActionAskRetryContinuesWhenCancelRequestItemFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(errors.New("cancel failed"))
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateValidated, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
	action := LenderActionAskRetry

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"itemId":      "0201896834",
			"reasonRetry": string(iso18626.ReasonRetryNotFoundAsCited),
		},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, LenderStateCompletedWithRetry, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.False(t, mockPrRepo.savedPr.NeedsAttention)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	compensation, ok := resultData.CustomData[compensationResultKey].(actionCompensationResult)
	if assert.True(t, ok) {
		assert.Equal(t, "CancelRequestItem", compensation.Operation)
		assert.Equal(t, ActionOutcomeFailure, compensation.Outcome)
		assert.Contains(t, compensation.Error, "cancel failed")
	}
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeBorrowerActionAcceptRetryAutoActionCreateTaskError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockEventBus := new(MockEventBus)
	mockEventBus.createTaskErr = errors.New("event bus error")
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(createLmsAdapterMockLog(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(MockIllRepo), mockEventBus, mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	assert.Equal(t, string(BorrowerActionValidatePatron), mockPrRepo.savedPr.LastAction.String)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateWillSupplyPending, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionAddCondition

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note": "Condition note",
		},
	}})
	assert.Equal(t, events.EventStatusError, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateWillSupplyPending, mockPrRepo.savedPr.State)
	assert.Equal(t, "loanCondition or cost is required", resultData.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	assert.Len(t, mockPrRepo.savedNotifications, 0)
}

func TestHandleInvokeLenderActionAddConditionWithCurrency(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateWillSupplyPending, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateWillSupplyPending, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
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
	assert.Equal(t, LenderStateWillSupplyPending, mockPrRepo.savedPr.State)
	assert.Equal(t, "currency is required when cost is provided", resultData.EventError.Message)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
}

func TestHandleInvokeLenderActionAddConditionTypeCost(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{IllRequest: illRequest, State: LenderStateWillSupplyPending, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
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
	assert.Equal(t, LenderStateWillSupplyPending, mockPrRepo.savedPr.State)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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

func TestHandleInvokeLenderActionShipCopyOrLoanCreatesDeferredLmsItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", mock.Anything, "item-1", mock.Anything, mock.Anything, mock.Anything).
		Return(&lms.RequestedItem{Barcode: "barcode-1", CallNumber: "call-1", Title: "title-1"}, nil).Once()
	lmsAdapter.On("CheckOutItem", "request-1", "barcode-1", mock.Anything, mock.Anything).Return("", nil).Once()
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:            iso18626.Header{RequestingAgencyRequestId: "request-1"},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "item-1"},
		ServiceInfo:       &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopyOrLoan},
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{}, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{PrID: patronRequestId, Barcode: "barcode-1", LmsRequestID: getDbText("request-1")}}, nil).Once()
	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateShipped, mockPrRepo.savedPr.State)
	assert.Len(t, mockPrRepo.savedItems, 1)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionShipCopyOrLoanCreatesDeferredLmsItemAlongsideManualItem(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", "request-1", "item-1", mock.Anything, mock.Anything, mock.Anything).
		Return(&lms.RequestedItem{Barcode: "requested-barcode", CallNumber: "call-1", Title: "title-1"}, nil).Once()
	lmsAdapter.On("CheckOutItem", "", "manual-barcode", mock.Anything, mock.Anything).Return("", nil).Once()
	lmsAdapter.On("CheckOutItem", "request-1", "requested-barcode", mock.Anything, mock.Anything).Return("", nil).Once()
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		Header:            iso18626.Header{RequestingAgencyRequestId: "request-1"},
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "item-1"},
		ServiceInfo:       &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopyOrLoan},
	}
	manualItem := pr_db.Item{ID: "manual-item", PrID: patronRequestId, Barcode: "manual-barcode"}
	requestedItem := pr_db.Item{ID: "requested-item", PrID: patronRequestId, Barcode: "requested-barcode", LmsRequestID: getDbText("request-1")}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{manualItem}, nil).Once()
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{manualItem, requestedItem}, nil).Once()
	action := LenderActionShip

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateShipped, mockPrRepo.savedPr.State)
	if assert.Len(t, mockPrRepo.savedItems, 1) {
		assert.Equal(t, "request-1", mockPrRepo.savedItems[0].LmsRequestID.String)
	}
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionSupplyDocument(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(new(mockLmsAdapter), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopy}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateWillSupply, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1")}, nil)
	action := LenderActionSupplyDocument

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note":        "Document ready",
			"deliveryUrl": "https://example.com/document/123",
		},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateCompleted, mockPrRepo.savedPr.State)
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		message := mockIso18626Handler.lastSupplyingAgencyMessage
		assert.Equal(t, iso18626.TypeStatusCopyCompleted, message.StatusInfo.Status)
		assert.Equal(t, "Document ready", message.MessageInfo.Note)
		if assert.NotNil(t, message.DeliveryInfo) {
			assert.Equal(t, "https://example.com/document/123", message.DeliveryInfo.ItemId)
			assert.Equal(t, string(iso18626.SentViaUrl), message.DeliveryInfo.SentVia.Text)
			assert.False(t, message.DeliveryInfo.DateSent.IsZero())
		}
	}
}

func TestSupplyDocumentRequiresDeliveryURL(t *testing.T) {
	prAction := &PatronRequestActionService{}

	result := prAction.supplyDocumentRequest(appCtx, "", pr_db.PatronRequest{}, actionParams{})

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "deliveryUrl is required", result.result.EventError.Message)
	assert.Equal(t, "deliveryUrl is required", result.result.EventError.Cause)
}

func TestSupplyDocumentRejectsInvalidDeliveryURL(t *testing.T) {
	prAction := &PatronRequestActionService{}

	result := prAction.supplyDocumentRequest(appCtx, "", pr_db.PatronRequest{}, actionParams{DeliveryURL: "not a URL"})

	assert.Equal(t, events.EventStatusError, result.status)
	assert.Equal(t, "deliveryUrl must be an absolute HTTP(S) URL", result.result.EventError.Message)
	assert.Equal(t, "deliveryUrl must be an absolute HTTP(S) URL", result.result.EventError.Cause)
}

func TestHandleInvokeLenderActionShipNewTitleOK(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CheckOutItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return("new title", nil)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateCancelRequested, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("req-1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
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

func TestHandleInvokeLenderActionAcceptCancelContinuesWhenCancelRequestItemFails(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("CancelRequestItem", "req-1", "").Return(errors.New("cancel failed"))
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{Header: iso18626.Header{RequestingAgencyRequestId: "req-1"}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateCancelRequested, Side: SideLending, SupplierSymbol: getDbText("ISIL:SUP1"), RequesterSymbol: getDbText("ISIL:REQ1"), RequesterReqID: getDbText("req-1")}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "item-1", LmsRequestID: getDbText("req-1")}}, nil)
	action := LenderActionAcceptCancel

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, LenderStateCancelled, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.False(t, mockPrRepo.savedPr.NeedsAttention)
	assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	compensation, ok := resultData.CustomData[compensationResultKey].(actionCompensationResult)
	if assert.True(t, ok) {
		assert.Equal(t, "CancelRequestItem", compensation.Operation)
		assert.Equal(t, ActionOutcomeFailure, compensation.Outcome)
		assert.Contains(t, compensation.Error, "cancel failed")
	}
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeLenderActionAcceptCancelMissingRequesterSymbol(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
			peer:           ill_db.Peer{CustomData: dirapi.Entry{}},
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "from email is not configured",
		},
		{
			name:           "fromEmail empty string",
			symbol:         "ISIL:A",
			toNeeded:       false,
			peer:           ill_db.Peer{CustomData: dirapi.Entry{FromEmail: &emptyStr}},
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "from email is not configured",
		},
		{
			name:     "toNeeded true uses fromEmail as recipient",
			symbol:   "ISIL:A",
			toNeeded: true,
			peer:     ill_db.Peer{CustomData: dirapi.Entry{FromEmail: &fromEmail}},
			wantFrom: fromEmail,
			wantTo:   &fromEmail,
		},
		{
			name:           "toNeeded true but fromEmail empty",
			symbol:         "ISIL:A",
			toNeeded:       true,
			peer:           ill_db.Peer{CustomData: dirapi.Entry{FromEmail: &emptyStr}},
			wantFrom:       "",
			wantTo:         &emptyStr,
			wantErrMessage: "from email is not configured",
		},
		{
			name:     "toNeeded false with no email configured",
			symbol:   "ISIL:A",
			toNeeded: false,
			peer:     ill_db.Peer{CustomData: dirapi.Entry{FromEmail: &fromEmail}},
			wantFrom: fromEmail,
			wantTo:   nil,
		},
		{
			name:     "toNeeded true with both emails configured",
			symbol:   "ISIL:A",
			toNeeded: true,
			peer:     ill_db.Peer{CustomData: dirapi.Entry{FromEmail: &fromEmail}},
			wantFrom: fromEmail,
			wantTo:   &fromEmail,
		},
		{
			name:     "toNeeded false with both emails configured",
			symbol:   "ISIL:A",
			toNeeded: false,
			peer:     ill_db.Peer{CustomData: dirapi.Entry{FromEmail: &fromEmail}},
			wantFrom: fromEmail,
			wantTo:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			illRepoMock := new(IllRepoMock)
			illRepoMock.On("GetPeerBySymbol", tc.symbol).Return(tc.peer, tc.repoErr)
			prAction := CreatePatronRequestActionService(*new(pr_db.PrRepo), illRepoMock, *new(events.EventBus), new(handler.Iso18626Handler), nil, new(EmailSenderMock), nil, nil)

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
		nil,
		nil,
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

	pr := pr_db.PatronRequest{IllRequest: iso18626.Request{}}

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
		{
			name:       "invalid template body",
			from:       "from@example.com",
			recipients: recipients,
			setupPrRepo: func(m *MockPrRepo) {
				m.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{
					Body:    "Hello patron {{.PatronEmai ",
					Subject: pgtype.Text{String: "Your request", Valid: true},
				}, nil)
			},
			setupEmail: func(m *EmailSenderMock) {},
			assertEmail: func(t *testing.T, m *EmailSenderMock) {
				m.AssertNotCalled(t, "SendEmail", mock.Anything)
			},
			wantErrSubstr: "template: pull-slip:1: unclosed action",
		},
		{
			name:       "invalid template body",
			from:       "from@example.com",
			recipients: recipients,
			setupPrRepo: func(m *MockPrRepo) {
				m.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{
					Body:    "Hello patron",
					Subject: pgtype.Text{String: "Your request {{.PatronEmai ", Valid: true},
				}, nil)
			},
			setupEmail: func(m *EmailSenderMock) {},
			assertEmail: func(t *testing.T, m *EmailSenderMock) {
				m.AssertNotCalled(t, "SendEmail", mock.Anything)
			},
			wantErrSubstr: "template: pull-slip:1: unclosed action",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockPrRepo := new(MockPrRepo)
			tc.setupPrRepo(mockPrRepo)
			mockEmail := new(EmailSenderMock)
			tc.setupEmail(mockEmail)
			svc := newActionServiceWithEmail(mockPrRepo, mockEmail)

			err := svc.createAndSendEmail(appCtx, pr, symbol, tc.from, tc.recipients, label, audience)

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
	return ill_db.Peer{CustomData: dirapi.Entry{
		FromEmail: ptr(fromEmail),
	}}
}

func peerWithFromEmailOnly(fromEmail string) ill_db.Peer {
	return ill_db.Peer{CustomData: dirapi.Entry{
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
			name:   "nil TemplateLabel – logged success",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: actionParams{AutoActionParams: &proapi.ModelAction_Params{
				SendTo: sendToTargets(proapi.ModelActionParamsSendToPatron),
			}},
			setupMocks: func(_ *MockPrRepo, _ *IllRepoMock, _ *EmailSenderMock) {},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "template label is not set",
		},
		{
			name:   "GetPeerBySymbol error – logged success",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToPatron),
			setupMocks: func(_ *MockPrRepo, illRepo *IllRepoMock, _ *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(ill_db.Peer{}, errors.New("db error"))
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "error getting directory email data",
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
			name:   "SendTo patron – SendEmail fails – logged success",
			pr:     prWithPatronEmail(testPatronTo),
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToPatron),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithFromEmailOnly(testFrom), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(errors.New("smtp error"))
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "error sending email to patron",
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
			name:   "SendTo staff – fromEmail is used as staff recipient",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithFromEmailOnly(testFrom), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(nil)
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "staff email sent successfully",
		},
		{
			name:   "SendTo staff – SendEmail fails – logged success",
			pr:     pr_db.PatronRequest{},
			symbol: testSymbol,
			params: autoParams(testTemplate, proapi.ModelActionParamsSendToStaff),
			setupMocks: func(prRepo *MockPrRepo, illRepo *IllRepoMock, emailSvc *EmailSenderMock) {
				illRepo.On("GetPeerBySymbol", testSymbol).Return(peerWithEmail(testFrom, testStaffTo), nil)
				prRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(foundTemplate, nil)
				emailSvc.On("SendEmail", testFrom).Return(errors.New("smtp error"))
			},
			wantStatus: events.EventStatusSuccess,
			wantNote:   "error sending email to staff",
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
				nil,
				nil,
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
		CustomData: dirapi.Entry{
			FromEmail: ptr("from@mail.com"),
		},
	}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock, nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{Body: "body", Subject: pgtype.Text{String: "subj", Valid: true}}, nil)

	action := BorrowerActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToPatron, proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("received-template"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, BorrowerStateReceived, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionBorrowerActionSendNotification_emailServiceNotReady(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REC1").Return(lms.CreateLmsAdapterMockOK(), nil)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(false)
	illMock := new(IllRepoMock)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock, nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: BorrowerStateReceived, Side: SideBorrowing, RequesterSymbol: pgtype.Text{Valid: true, String: "ISIL:REC1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := BorrowerActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToPatron, proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("received-template"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, "email service is not ready to send", resultData.Note)
	assert.Equal(t, BorrowerStateReceived, mockPrRepo.savedPr.State)
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
		CustomData: dirapi.Entry{
			FromEmail: ptr("from@mail.com"),
		},
	}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock, nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateNew, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)
	mockPrRepo.On("GetTemplateByPurposeAudienceLabelAndOwner", mock.Anything).Return(pr_db.Template{Body: "body", Subject: pgtype.Text{String: "subj", Valid: true}}, nil)

	action := LenderActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("new-supply-request-notification"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, LenderStateNew, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionLenderActionSendNotification_emailServiceNotReady(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:SUP1").Return(lms.CreateLmsAdapterMockOK(), nil)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(false)
	illMock := new(IllRepoMock)
	illMock.On("GetPeerBySymbol", "ISIL:SUP1").Return(ill_db.Peer{
		CustomData: dirapi.Entry{
			FromEmail: ptr("from@mail.com"),
		},
	}, nil)
	prAction := CreatePatronRequestActionService(mockPrRepo, illMock, *new(events.EventBus), new(handler.Iso18626Handler), lmsCreator, emailMock, nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{ID: patronRequestId, IllRequest: illRequest, State: LenderStateNew, Side: SideLending, SupplierSymbol: pgtype.Text{Valid: true, String: "ISIL:SUP1"}}, nil)
	mockPrRepo.On("GetItemsByPrId", patronRequestId).Return([]pr_db.Item{{Barcode: "1234"}}, nil)

	action := LenderActionSendNotification
	data := map[string]any{"autoActionParams": proapi.ModelAction_Params{
		SendTo:        &[]proapi.ModelActionParamsSendTo{proapi.ModelActionParamsSendToStaff},
		TemplateLabel: ptr("new-supply-request-notification"),
	}}
	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &action}, CustomData: data}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.NotNil(t, resultData)
	assert.Equal(t, "email service is not ready to send", resultData.Note)
	assert.Equal(t, LenderStateNew, mockPrRepo.savedPr.State)
}

func TestHandleInvokeBorrowerActionCancelLocalSupply(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(lms.CreateLmsAdapterMockOK(), nil)
	mockIso18626Handler := new(MockIso18626Handler)
	mockEventBus := new(MockEventBus)
	emailMock := new(EmailSenderMock)
	emailMock.On("IsReadyToSend").Return(false)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), mockEventBus, mockIso18626Handler, lmsCreator, emailMock, nil, nil)
	illRequest := iso18626.Request{}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Once().Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           BorrowerStateLocalSupply,
		Side:            SideBorrowing,
		SupplierSymbol:  getDbText("ISIL:SUP1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		RequesterReqID:  getDbText("req-1"),
	}, nil)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           BorrowerStateCancelled,
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
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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
		{name: "copy or loan", serviceType: iso18626.TypeServiceTypeCopyOrLoan, expectedStatus: iso18626.TypeStatusLoanCompleted},
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
					Return(&lms.RequestedItem{Barcode: "item-barcode"}, nil)
				lmsAdapter = adapterMock
			}
			lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(lmsAdapter, nil)
			mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr, nil)
			prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
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

func TestHandleInvokeBorrowerActionFillLocallyCancelsRequestItemWithoutBarcode(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsAdapter.On("RequestItem", patronRequestId, "local-record-1", "patron-1", "", "").
		Return(&lms.RequestedItem{RequestID: "lms-req-1"}, nil).Once()
	lmsAdapter.On("CancelRequestItem", "lms-req-1", "patron-1").Return(nil).Once()
	lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{SupplierUniqueRecordId: "local-record-1"},
		ServiceInfo:       &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           BorrowerStateLocalSupply,
		Side:            SideBorrowing,
		Patron:          getDbText("patron-1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		SupplierSymbol:  getDbText("ISIL:REQ1"),
	}, nil)
	action := BorrowerActionFillLocally

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{
		PatronRequestID: patronRequestId,
		EventData:       events.EventData{CommonEventData: events.CommonEventData{Action: &action}},
	})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "LMS RequestItem returned an empty barcode", resultData.EventError.Message)
	assert.Equal(t, BorrowerStateLocalSupply, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Nil(t, mockIso18626Handler.lastSupplyingAgencyMessage)
	lmsAdapter.AssertExpectations(t)
}

func TestHandleInvokeBorrowerActionSupplyDocument(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	lmsCreator := new(MockLmsCreator)
	lmsAdapter := new(mockLmsAdapter)
	lmsCreator.On("GetAdapter", "ISIL:REQ1").Return(lmsAdapter, nil)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestActionService(mockPrRepo, new(IllRepoMock), *new(events.EventBus), mockIso18626Handler, lmsCreator, new(EmailSenderMock), nil, nil)
	illRequest := iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeCopy}}
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{
		ID:              patronRequestId,
		IllRequest:      illRequest,
		State:           BorrowerStateLocalSupply,
		Side:            SideBorrowing,
		SupplierSymbol:  getDbText("ISIL:REQ1"),
		RequesterSymbol: getDbText("ISIL:REQ1"),
		RequesterReqID:  getDbText("req-1"),
		NeedsAttention:  true,
	}, nil)
	action := BorrowerActionSupplyDocument

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{
		CommonEventData: events.CommonEventData{Action: &action},
		CustomData: map[string]any{
			"note":        "Local document ready",
			"deliveryUrl": "https://example.com/local-document/123",
		},
	}})

	assert.Equal(t, events.EventStatusSuccess, status)
	if assert.NotNil(t, resultData.ActionResult) && assert.NotNil(t, resultData.ActionResult.ToState) {
		assert.Equal(t, string(BorrowerStateCompleted), *resultData.ActionResult.ToState)
	}
	assert.Equal(t, BorrowerStateCompleted, mockPrRepo.savedPr.State)
	assert.True(t, mockPrRepo.savedPr.TerminalState)
	assert.False(t, mockPrRepo.savedPr.NeedsAttention)
	assert.Equal(t, iso18626.TypeStatusCopyCompleted, mockPrRepo.savedPr.IllResponse.StatusInfo.Status)
	if assert.NotNil(t, mockPrRepo.savedPr.IllResponse.DeliveryInfo) {
		assert.Equal(t, "https://example.com/local-document/123", mockPrRepo.savedPr.IllResponse.DeliveryInfo.ItemId)
	}
	if assert.NotNil(t, mockIso18626Handler.lastSupplyingAgencyMessage) {
		message := mockIso18626Handler.lastSupplyingAgencyMessage
		assert.Equal(t, iso18626.TypeStatusCopyCompleted, message.StatusInfo.Status)
		assert.Equal(t, "Local document ready", message.MessageInfo.Note)
		if assert.NotNil(t, message.DeliveryInfo) {
			assert.Equal(t, "https://example.com/local-document/123", message.DeliveryInfo.ItemId)
			assert.Equal(t, string(iso18626.SentViaUrl), message.DeliveryInfo.SentVia.Text)
		}
	}
	lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
}

func TestUpdateMetadataBorrowingRequestAddsDecisionDetails(t *testing.T) {
	mode := dirapi.Auto
	configPeerID := uuid.MustParse("00000000-0000-0000-0000-000000000456")
	illRepo := new(IllRepoMock)
	illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:x"}, mock.Anything).Return([]ill_db.Peer{
		{
			Vendor: string(dirapi.CrossLink),
			CustomData: dirapi.Entry{
				Id: &configPeerID,
				CatalogConfig: &dirapi.CatalogConfig{
					MetadataUpdateMode: &mode,
					Sru:                &dirapi.SruConfig{Address: "http://sru.example.test"},
				},
			},
		},
	}, "", nil)
	svc := &PatronRequestActionService{
		illRepo: illRepo,
		lookupAdapterFactory: lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Metadata: catalog.Metadata{
			Identifier: "catalog-record-456",
			Title:      "Canonical title",
			Author:     "Example Author",
			Isbn:       "9781234567890",
		}}),
	}
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		RequesterSymbol: pgtype.Text{String: "ISIL:x", Valid: true},
	}
	illRequest := iso18626.Request{
		BibliographicInfo: iso18626.BibliographicInfo{
			Title:                  "Original title",
			SupplierUniqueRecordId: "record-123",
			BibliographicItemId: []iso18626.BibliographicItemId{
				{
					BibliographicItemIdentifier:     "9781234567890",
					BibliographicItemIdentifierCode: iso18626.TypeSchemeValuePair{Text: "ISBN"},
				},
			},
		},
		ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan},
	}

	res := svc.updateMetadataBorrowingRequest(appCtx, pr, illRequest)

	assert.Equal(t, events.EventStatusSuccess, res.status)
	if assert.NotNil(t, res.result) {
		details, ok := res.result.CustomData["decisionDetails"].([]actionDecisionDetailMetadataUpdate)
		if assert.True(t, ok) && assert.Len(t, details, 1) {
			assert.Equal(t, actionDecisionDetailMetadataUpdate{
				Type:          "metadata-update",
				Outcome:       "updated",
				Mode:          "auto",
				EffectiveMode: "replace",
				LookupParams: catalog.LookupParams{
					Identifier:  "record-123",
					Isbn:        "9781234567890",
					Issn:        "",
					Title:       "Original title",
					ServiceType: "Loan",
				},
				Source: actionDecisionDetailMetadataSource{
					AdapterType:         "sru",
					ConfigurationPeerID: "00000000-0000-0000-0000-000000000456",
				},
				Changes: []actionDecisionDetailMetadataChange{
					{Field: "title", PreviousValue: "Original title", NewValue: "Canonical title"},
					{Field: "author", PreviousValue: "", NewValue: "Example Author"},
					{Field: "supplierUniqueRecordId", PreviousValue: "record-123", NewValue: "catalog-record-456"},
				},
			}, details[0])
		}
	}
	illRepo.AssertExpectations(t)
}

func TestUpdateMetadataBorrowingRequestNegativeCases(t *testing.T) {
	pr := pr_db.PatronRequest{
		ID:              patronRequestId,
		RequesterSymbol: pgtype.Text{String: "ISIL:x", Valid: true},
	}

	tests := []struct {
		name       string
		setup      func(*IllRepoMock) *PatronRequestActionService
		wantMsg    string
		wantErrMsg string
	}{
		{
			name: "requester peer lookup fails",
			setup: func(illRepo *IllRepoMock) *PatronRequestActionService {
				illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:x"}, mock.Anything).Return([]ill_db.Peer{}, "", errors.New("peer lookup failed"))
				return &PatronRequestActionService{
					illRepo: illRepo,
				}
			},
			wantMsg:    "failed to get requester peer",
			wantErrMsg: "peer lookup failed",
		},
		{
			name: "requester peer not found",
			setup: func(illRepo *IllRepoMock) *PatronRequestActionService {
				illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:x"}, mock.Anything).Return([]ill_db.Peer{}, "", nil)
				return &PatronRequestActionService{
					illRepo: illRepo,
				}
			},
			wantMsg:    "failed to get requester peer",
			wantErrMsg: "no peer found for requester symbol \"ISIL:x\"",
		},
		{
			name: "metadata update fails for CrossLink requester peer",
			setup: func(illRepo *IllRepoMock) *PatronRequestActionService {
				mode := dirapi.Merge
				illRepo.On("GetCachedPeersBySymbols", []string{"ISIL:x"}, mock.Anything).Return([]ill_db.Peer{
					{
						Vendor:     string(dirapi.CrossLink),
						CustomData: dirapi.Entry{Name: "requester", CatalogConfig: &dirapi.CatalogConfig{MetadataUpdateMode: &mode}},
					},
				}, "", nil)
				return &PatronRequestActionService{
					illRepo:              illRepo,
					lookupAdapterFactory: lookupFactoryWithAdapter(&catalog.MockLookupAdapter{Err: errors.New("lookup failed")}),
				}
			},
			wantMsg:    "metadata update failed",
			wantErrMsg: "failed to perform lookup for patron request: lookup failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			illRepo := new(IllRepoMock)
			svc := tt.setup(illRepo)

			res := svc.updateMetadataBorrowingRequest(appCtx, pr, iso18626.Request{})

			assert.Equal(t, events.EventStatusError, res.status)
			assert.Equal(t, pr, res.pr)
			if assert.NotNil(t, res.result) {
				if assert.NotNil(t, res.result.EventError) {
					assert.Equal(t, tt.wantMsg, res.result.EventError.Message)
					assert.Equal(t, tt.wantErrMsg, res.result.EventError.Cause)
				}
				if assert.NotNil(t, res.result.ActionResult) {
					assert.Equal(t, ActionOutcomeFailure, res.result.ActionResult.Outcome)
				}
			}
			illRepo.AssertExpectations(t)
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
	createdNoticeNames  []events.EventName
	createdNoticeData   []events.EventData
	createdNoticeStatus []events.EventStatus
	createdNoticeParent []string
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
	m.createdNoticeNames = append(m.createdNoticeNames, eventName)
	m.createdNoticeData = append(m.createdNoticeData, data)
	m.createdNoticeStatus = append(m.createdNoticeStatus, status)
	m.createdNoticeParent = append(m.createdNoticeParent, "")
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
	m.createdNoticeNames = append(m.createdNoticeNames, eventName)
	m.createdNoticeData = append(m.createdNoticeData, data)
	m.createdNoticeStatus = append(m.createdNoticeStatus, status)
	m.createdNoticeParent = append(m.createdNoticeParent, *parentId)
	for _, call := range m.ExpectedCalls {
		if call.Method == "CreateNoticeWithParent" && len(call.Arguments) > 0 && call.Arguments.Get(0) == *parentId {
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
	deletedItemIDs                       []string
	saveItemFail                         bool
	deleteItemFail                       bool
	saveNotificationFail                 bool
	lastListQuery                        pgcql.Query
	requesterLmsItemCreated              map[string]bool
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

func (r *MockPrRepo) ListPatronRequests(ctx common.ExtendedContext, params pr_db.ListPatronRequestsParams, query pgcql.Query) ([]pr_db.PatronRequest, int64, error) {
	r.lastListQuery = query
	args := r.Called(params, query)
	return args.Get(0).([]pr_db.PatronRequest), args.Get(1).(int64), args.Error(2)
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
	item := pr_db.Item{
		ID:           params.ID,
		PrID:         params.PrID,
		Barcode:      params.Barcode,
		CallNumber:   params.CallNumber,
		Title:        params.Title,
		ItemID:       params.ItemID,
		LmsRequestID: params.LmsRequestID,
		CreatedAt:    params.CreatedAt,
	}
	r.savedItems = append(r.savedItems, item)
	return item, nil
}

func (r *MockPrRepo) GetItemsByPrId(ctx common.ExtendedContext, id string) ([]pr_db.Item, error) {
	for _, call := range r.ExpectedCalls {
		if call.Method == "GetItemsByPrId" {
			args := r.Called(id)
			return args.Get(0).([]pr_db.Item), args.Error(1)
		}
	}
	items := make([]pr_db.Item, 0)
	for _, item := range r.savedItems {
		if item.PrID == id {
			items = append(items, item)
		}
	}
	return items, nil
}

func (r *MockPrRepo) SetRequesterLmsItemCreated(ctx common.ExtendedContext, params pr_db.SetRequesterLmsItemCreatedParams) error {
	for _, call := range r.ExpectedCalls {
		if call.Method == "SetRequesterLmsItemCreated" {
			return r.Called(params).Error(0)
		}
	}
	if r.requesterLmsItemCreated == nil {
		r.requesterLmsItemCreated = make(map[string]bool)
	}
	r.requesterLmsItemCreated[params.ID] = params.RequesterLmsItemCreated
	return nil
}

func (r *MockPrRepo) DeleteItemById(ctx common.ExtendedContext, id string) error {
	if r.deleteItemFail {
		return errors.New("db error")
	}
	r.deletedItemIDs = append(r.deletedItemIDs, id)
	for i, item := range r.savedItems {
		if item.ID == id {
			r.savedItems = append(r.savedItems[:i], r.savedItems[i+1:]...)
			break
		}
	}
	return nil
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
	if r.saveNotificationFail || params.PrID == "error" {
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
	failSupplyingAgencyMessage  bool
}

func (h *MockIso18626Handler) HandleRequest(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter) map[string]any {
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
		return nil
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
	return nil
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
	if h.failSupplyingAgencyMessage || illMessage.SupplyingAgencyMessage.Header.RequestingAgencyRequestId == "error" {
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
	logFunc        ncipclient.NcipLogFunc
	requestItemErr error
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
) (*lms.RequestedItem, error) {
	if l.requestItemErr != nil {
		return nil, l.requestItemErr
	}
	return &lms.RequestedItem{Barcode: "item-barcode"}, nil
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

type MockLmsAdapterPatronProblem struct {
	MockLmsAdapterFail
}

func (l *MockLmsAdapterPatronProblem) LookupUser(patron string) (string, error) {
	return "", &ncipclient.NcipError{
		Message: "NCIP user lookup failed",
		Problem: ncip.Problem{
			ProblemType:   ncip.SchemeValuePair{Text: string(ncip.UnknownUser)},
			ProblemDetail: "patron was not found",
		},
	}
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
) (*lms.RequestedItem, error) {
	return nil, errors.New("RequestItem failed")
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

func TestLoadDefaultStateModel(t *testing.T) {
	stateModel, err := LoadStateModelByName("default")
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
) (*lms.RequestedItem, error) {
	args := m.Called(requestId, itemId, userId, pickupLocation, itemLocation)
	requestedItem, _ := args.Get(0).(*lms.RequestedItem)
	return requestedItem, args.Error(1)
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

func (m *mockLmsAdapter) DeleteItem(itemId string) error {
	return m.Called(itemId).Error(0)
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

func (i *IllRepoMock) GetCachedPeersBySymbols(ctx common.ExtendedContext, symbols []string, directoryAdapter adapter.DirectoryLookupAdapter) ([]ill_db.Peer, string, error) {
	for _, call := range i.ExpectedCalls {
		if call.Method == "GetCachedPeersBySymbols" {
			args := i.Called(symbols, directoryAdapter)
			return args.Get(0).([]ill_db.Peer), args.String(1), args.Error(2)
		}
	}
	return []ill_db.Peer{{Vendor: "other"}}, "", nil
}

// --- metadataUpdate tests ---

// mockLookupCreator controls what GetAdapter returns when no globalLookupAdapter is pre-set.
type mockLookupCreator struct {
	adapter catalog.LookupAdapter
	err     error
}

func (m *mockLookupCreator) GetAdapter(peer ill_db.Peer) (catalog.LookupAdapter, error) {
	return m.adapter, m.err
}

// lookupFactoryWithAdapter creates a LookupAdapterFactory that returns the given adapter directly.
func lookupFactoryWithAdapter(adapter catalog.LookupAdapter) *service.LookupAdapterFactory {
	return service.NewLookupAdapterFactory(nil, nil, "", adapter, nil)
}

// peerWithMetadataMode builds a Peer whose CustomData carries the given MetadataUpdateMode.
// Pass nil to leave CatalogConfig absent entirely.
func peerWithMetadataMode(mode *dirapi.MetadataUpdateMode) ill_db.Peer {
	var cc *dirapi.CatalogConfig
	if mode != nil {
		cc = &dirapi.CatalogConfig{MetadataUpdateMode: mode}
	}
	return ill_db.Peer{
		CustomData: dirapi.Entry{Name: "test-peer", CatalogConfig: cc},
	}
}
