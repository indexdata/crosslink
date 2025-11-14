package prservice

import (
	"context"
	"encoding/xml"
	"errors"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"net/http"
	"strings"
	"testing"
)

var appCtx = common.CreateExtCtxWithArgs(context.Background(), nil)
var patronRequestId = "pr1"

func TestGetBorrowerActionsByState(t *testing.T) {
	assert.Equal(t, []string{ActionValidate}, GetBorrowerActionsByState(BorrowerStateNew))
	assert.Equal(t, []string{}, GetBorrowerActionsByState(BorrowerStateCompleted))
}

func TestIsBorrowerActionAvailable(t *testing.T) {
	assert.True(t, IsBorrowerActionAvailable(BorrowerStateNew, ActionValidate))
	assert.False(t, IsBorrowerActionAvailable(BorrowerStateNew, ActionCheckOut))
	assert.False(t, IsBorrowerActionAvailable(BorrowerStateCompleted, ActionValidate))
}
func TestInvokeAction(t *testing.T) {
	mockEventBus := new(MockEventBus)
	prAction := CreatePatronRequestAction(*new(pr_db.PrRepo), *new(ill_db.IllRepo), mockEventBus, new(handler.Iso18626Handler))
	event := events.Event{
		ID: "action-1",
	}
	mockEventBus.On("ProcessTask", event.ID).Return(event, nil)

	prAction.InvokeAction(appCtx, event)

	mockEventBus.AssertNumberOfCalls(t, "ProcessTask", 1)
}

func TestHandleInvokeActionNotSpecifiedAction(t *testing.T) {
	prAction := CreatePatronRequestAction(*new(pr_db.PrRepo), *new(ill_db.IllRepo), *new(events.EventBus), new(handler.Iso18626Handler))

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "action not specified", resultData.EventError.Message)
}

func TestHandleInvokeActionNoPR(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestAction(mockPrRepo, *new(ill_db.IllRepo), *new(events.EventBus), new(handler.Iso18626Handler))
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{}, errors.New("not fund"))

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "failed to read patron request", resultData.EventError.Message)
}

func TestHandleInvokeActionWhichIsNotAllowed(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestAction(mockPrRepo, *new(ill_db.IllRepo), *new(events.EventBus), new(handler.Iso18626Handler))
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateValidated}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionValidate}}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "state VALIDATED does not support action validate", resultData.EventError.Message)
}

func TestHandleInvokeActionValidate(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestAction(mockPrRepo, *new(ill_db.IllRepo), *new(events.EventBus), new(handler.Iso18626Handler))
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateNew}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionValidate}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateValidated, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionSendRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "peer1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionSendRequest}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateSent, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionReceive(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateShipped, BorrowingPeerID: pgtype.Text{Valid: true, String: "peer1"}, LendingPeerID: pgtype.Text{Valid: true, String: "peer1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionReceive}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateReceived, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckOut(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestAction(mockPrRepo, *new(ill_db.IllRepo), *new(events.EventBus), new(handler.Iso18626Handler))
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateReceived}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionCheckOut}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedOut, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCheckIn(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	prAction := CreatePatronRequestAction(mockPrRepo, *new(ill_db.IllRepo), *new(events.EventBus), new(handler.Iso18626Handler))
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateCheckedOut}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionCheckIn}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCheckedIn, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionShipReturn(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateCheckedIn, BorrowingPeerID: pgtype.Text{Valid: true, String: "peer1"}, LendingPeerID: pgtype.Text{Valid: true, String: "peer1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionShipReturn}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateShippedReturned, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionCancelRequest(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateWillSupply, BorrowingPeerID: pgtype.Text{Valid: true, String: "peer1"}, LendingPeerID: pgtype.Text{Valid: true, String: "peer1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionCancelRequest}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Equal(t, iso18626.TypeMessageStatusOK, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionAcceptCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateConditionPending, BorrowingPeerID: pgtype.Text{Valid: true, String: "peer1"}, LendingPeerID: pgtype.Text{Valid: true, String: "peer1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionAcceptCondition}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateWillSupply, mockPrRepo.savedPr.State)
}

func TestHandleInvokeActionRejectCondition(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)
	mockPrRepo.On("GetPatronRequestById", patronRequestId).Return(pr_db.PatronRequest{State: BorrowerStateConditionPending, BorrowingPeerID: pgtype.Text{Valid: true, String: "peer1"}, LendingPeerID: pgtype.Text{Valid: true, String: "peer1"}}, nil)

	status, resultData := prAction.handleInvokeAction(appCtx, events.Event{PatronRequestID: patronRequestId, EventData: events.EventData{CommonEventData: events.CommonEventData{Action: &ActionRejectCondition}}})

	assert.Equal(t, events.EventStatusSuccess, status)
	assert.Nil(t, resultData)
	assert.Equal(t, BorrowerStateCancelPending, mockPrRepo.savedPr.State)
}

func TestSendBorrowingRequestNoRequester(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{State: BorrowerStateValidated})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing borrowing peer id", resultData.EventError.Message)
}

func TestSendBorrowingRequestSymbolSearchError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "error"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "cannot fetch borrowing peer symbols", resultData.EventError.Message)
}

func TestSendBorrowingRequestSymbolMissing(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "missing"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing borrowing peer symbols", resultData.EventError.Message)
}

func TestSendBorrowingRequestFailedProcess(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.sendBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "error", State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "pr123"}})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resultData.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus)
}

func TestShipReturnBorrowingRequestNoBorrowingId(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "1", State: BorrowerStateValidated})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing borrowing peer id", resultData.EventError.Message)
}
func TestShipReturnBorrowingRequestErrorBorrowing(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "1", State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "error"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "cannot fetch borrowing peer symbols", resultData.EventError.Message)
}
func TestShipReturnBorrowingRequestMissingBorrowing(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "1", State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "missing"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing borrowing peer symbols", resultData.EventError.Message)
}
func TestShipReturnBorrowingRequestNoLendingId(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "1", State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "pr123"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing lending peer id", resultData.EventError.Message)
}
func TestShipReturnBorrowingRequestLendingError(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "1", State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "pr123"}, LendingPeerID: pgtype.Text{Valid: true, String: "error"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "cannot fetch lending peer symbols", resultData.EventError.Message)
}
func TestShipReturnBorrowingRequestLendingMissing(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "1", State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "pr123"}, LendingPeerID: pgtype.Text{Valid: true, String: "missing"}})

	assert.Equal(t, events.EventStatusError, status)
	assert.Equal(t, "missing lending peer symbols", resultData.EventError.Message)
}
func TestShipReturnBorrowingRequestProblemProcessing(t *testing.T) {
	mockPrRepo := new(MockPrRepo)
	mockIso18626Handler := new(MockIso18626Handler)
	prAction := CreatePatronRequestAction(mockPrRepo, new(MockIllRepo), *new(events.EventBus), mockIso18626Handler)

	status, resultData := prAction.shipReturnBorrowingRequest(appCtx, pr_db.PatronRequest{ID: "error", State: BorrowerStateValidated, BorrowingPeerID: pgtype.Text{Valid: true, String: "pr123"}, LendingPeerID: pgtype.Text{Valid: true, String: "pr321"}})

	assert.Equal(t, events.EventStatusProblem, status)
	assert.Equal(t, iso18626.TypeMessageStatusERROR, resultData.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus)
}

type MockEventBus struct {
	mock.Mock
	events.EventBus
}

func (m *MockEventBus) ProcessTask(ctx common.ExtendedContext, event events.Event, h func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	args := m.Called(event.ID)
	return args.Get(0).(events.Event), args.Error(1)
}

func (m *MockEventBus) CreateTask(id string, eventName events.EventName, data events.EventData, eventClass events.EventClass, parentId *string) (string, error) {
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
	if strings.Contains(params.ID, "error") {
		return pr_db.PatronRequest{}, errors.New("db error")
	}
	r.savedPr = pr_db.PatronRequest(params)
	return pr_db.PatronRequest(params), nil
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

type MockIllRepo struct {
	mock.Mock
	ill_db.PgIllRepo
}

func (r *MockIllRepo) GetSymbolsByPeerId(ctx common.ExtendedContext, peerId string) ([]ill_db.Symbol, error) {
	if peerId == "error" {
		return []ill_db.Symbol{}, errors.New("db error")
	}
	if peerId == "missing" {
		return nil, nil
	}
	return []ill_db.Symbol{{SymbolValue: "ISIL:PEER1", PeerID: peerId}}, nil
}

func (r *MockIllRepo) GetPeerBySymbol(ctx common.ExtendedContext, symbol string) (ill_db.Peer, error) {
	args := r.Called(symbol)
	return args.Get(0).(ill_db.Peer), args.Error(1)
}
