package prservice

import (
	"encoding/xml"
	"errors"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"net/http"
	"slices"
	"strings"
)

const COMP = "pr_action_service"

var SideBorrowing = "borrowing"
var SideLanding = "landing"

var BorrowerStateNew = "NEW"
var BorrowerStateValidated = "VALIDATED"
var BorrowerStateSent = "SENT"
var BorrowerStateSupplierLocated = "SUPPLIER_LOCATED"
var BorrowerStateConditionPending = "CONDITION_PENDING"
var BorrowerStateWillSupply = "WILL_SUPPLY"
var BorrowerStateShipped = "SHIPPED"
var BorrowerStateReceived = "RECEIVED"
var BorrowerStateCheckedOut = "CHECKED_OUT"
var BorrowerStateCheckedIn = "CHECKED_IN"
var BorrowerStateShippedReturned = "SHIPPED_RETURNED"
var BorrowerStateCancelPending = "CANCEL_PENDING"
var BorrowerStateCompleted = "COMPLETED"
var BorrowerStateCancelled = "CANCELLED"
var BorrowerStateUnfilled = "UNFILLED"

var ActionValidate = "validate"
var ActionSendRequest = "send-request"
var ActionCancelRequest = "cancel-request"
var ActionAcceptCondition = "accept-condition"
var ActionRejectCondition = "reject-condition"
var ActionReceive = "receive"
var ActionCheckOut = "check-out"
var ActionCheckIn = "check-in"
var ActionShipReturn = "ship-return"

var BorrowerStateActionMapping = map[string][]string{
	BorrowerStateNew:              {ActionValidate},
	BorrowerStateValidated:        {ActionSendRequest},
	BorrowerStateSupplierLocated:  {ActionCancelRequest},
	BorrowerStateConditionPending: {ActionAcceptCondition, ActionRejectCondition},
	BorrowerStateWillSupply:       {ActionCancelRequest},
	BorrowerStateShipped:          {ActionReceive},
	BorrowerStateReceived:         {ActionCheckOut},
	BorrowerStateCheckedOut:       {ActionCheckIn},
	BorrowerStateCheckedIn:        {ActionShipReturn},
}

type PatronRequestActionService struct {
	prRepo          pr_db.PrRepo
	illRepo         ill_db.IllRepo
	eventBus        events.EventBus
	iso18626Handler handler.Iso18626HandlerInterface
}

func CreatePatronRequestAction(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo, eventBus events.EventBus, iso18626Handler handler.Iso18626HandlerInterface) PatronRequestActionService {
	return PatronRequestActionService{
		prRepo:          prRepo,
		illRepo:         illRepo,
		eventBus:        eventBus,
		iso18626Handler: iso18626Handler,
	}
}
func GetBorrowerActionsByState(state string) []string {
	if actions, ok := BorrowerStateActionMapping[state]; ok {
		return actions
	} else {
		return []string{}
	}
}

func IsBorrowerActionAvailable(state string, action string) bool {
	return slices.Contains(GetBorrowerActionsByState(state), action)
}

func (a *PatronRequestActionService) InvokeAction(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	_, _ = a.eventBus.ProcessTask(ctx, event, a.handleInvokeAction)
}

func (a *PatronRequestActionService) handleInvokeAction(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	if event.EventData.Action == nil {
		return events.LogErrorAndReturnResult(ctx, "action not specified", errors.New("action not specified"))
	}
	action := *event.EventData.Action
	pr, err := a.prRepo.GetPatronRequestById(ctx, event.PatronRequestID)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to read patron request", err)
	}
	if !IsBorrowerActionAvailable(pr.State, action) {
		return events.LogErrorAndReturnResult(ctx, "state "+pr.State+" does not support action "+action, errors.New("invalid action"))
	}

	switch action {
	case ActionValidate:
		return a.validateBorrowingRequest(ctx, pr)
	case ActionSendRequest:
		return a.sendBorrowingRequest(ctx, pr)
	case ActionReceive:
		return a.receiveBorrowingRequest(ctx, pr)
	case ActionCheckOut:
		return a.checkoutBorrowingRequest(ctx, pr)
	case ActionCheckIn:
		return a.checkinBorrowingRequest(ctx, pr)
	case ActionShipReturn:
		return a.shipReturnBorrowingRequest(ctx, pr)
	case ActionCancelRequest:
		return a.cancelBorrowingRequest(ctx, pr)
	case ActionAcceptCondition:
		return a.acceptConditionBorrowingRequest(ctx, pr)
	case ActionRejectCondition:
		return a.rejectConditionBorrowingRequest(ctx, pr)
	default:
		return events.LogErrorAndReturnResult(ctx, "action "+action+" is not implemented yet", errors.New("invalid action"))
	}
}
func (a *PatronRequestActionService) updateStateAndReturnResult(ctx common.ExtendedContext, pr pr_db.PatronRequest, state string, result *events.EventResult) (events.EventStatus, *events.EventResult) {
	pr.State = state
	pr, err := a.prRepo.SavePatronRequest(ctx, pr_db.SavePatronRequestParams(pr))
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to update patron request", err)
	}
	return events.EventStatusSuccess, result
}

func (a *PatronRequestActionService) validateBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	// TODO do validation

	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateValidated, nil)
}

func (a *PatronRequestActionService) sendBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	if !pr.BorrowingPeerID.Valid {
		return events.LogErrorAndReturnResult(ctx, "missing borrowing peer id", nil)
	}
	borrowerSymbols, err := a.illRepo.GetSymbolsByPeerId(ctx, pr.BorrowingPeerID.String)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "cannot fetch borrowing peer symbols", err)
	}
	if borrowerSymbols == nil {
		return events.LogErrorAndReturnResult(ctx, "missing borrowing peer symbols", err)
	}
	borrowerSymbol := strings.SplitN(borrowerSymbols[0].SymbolValue, ":", 2)
	var illMessage = iso18626.ISO18626Message{
		Request: &iso18626.Request{
			Header: iso18626.Header{
				RequestingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: borrowerSymbol[0],
					},
					AgencyIdValue: borrowerSymbol[1],
				},
				RequestingAgencyRequestId: pr.ID,
			},
			PatronInfo: &iso18626.PatronInfo{PatronId: pr.Requester.String},
			BibliographicInfo: iso18626.BibliographicInfo{
				SupplierUniqueRecordId: "WILLSUPPLY_LOANED",
			},
		},
	}
	w := NewResponseCaptureWriter()
	a.iso18626Handler.HandleRequest(ctx, &illMessage, w)
	result.OutgoingMessage = &illMessage
	result.IncomingMessage = w.IllMessage
	if w.StatusCode != http.StatusOK || w.IllMessage == nil || w.IllMessage.RequestConfirmation == nil ||
		w.IllMessage.RequestConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return events.EventStatusProblem, &result
	}
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateSent, &result)
}

func (a *PatronRequestActionService) receiveBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionReceived)
	if httpStatus == nil {
		return status, eventResult
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return events.EventStatusProblem, &result
	}
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateReceived, &result)
}

func (a *PatronRequestActionService) checkoutBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	// TODO Make NCIP calls
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateCheckedOut, nil)
}

func (a *PatronRequestActionService) checkinBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	// TODO Make NCIP calls
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateCheckedIn, nil)
}

func (a *PatronRequestActionService) shipReturnBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionShippedReturn)
	if httpStatus == nil {
		return status, eventResult
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return events.EventStatusProblem, &result
	}
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateShippedReturned, &result)
}

func (a *PatronRequestActionService) sendRequestingAgencyMessage(ctx common.ExtendedContext, pr pr_db.PatronRequest, result *events.EventResult, action iso18626.TypeAction) (events.EventStatus, *events.EventResult, *int) {
	if !pr.BorrowingPeerID.Valid {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing borrowing peer id", nil)
		return status, eventResult, nil
	}
	borrowerSymbols, err := a.illRepo.GetSymbolsByPeerId(ctx, pr.BorrowingPeerID.String)
	if err != nil {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "cannot fetch borrowing peer symbols", err)
		return status, eventResult, nil
	}
	if borrowerSymbols == nil {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing borrowing peer symbols", err)
		return status, eventResult, nil
	}
	if !pr.LendingPeerID.Valid {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing lending peer id", nil)
		return status, eventResult, nil
	}
	lenderSymbols, err := a.illRepo.GetSymbolsByPeerId(ctx, pr.LendingPeerID.String)
	if err != nil {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "cannot fetch lending peer symbols", err)
		return status, eventResult, nil
	}
	if lenderSymbols == nil {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing lending peer symbols", err)
		return status, eventResult, nil
	}
	borrowerSymbol := strings.SplitN(borrowerSymbols[0].SymbolValue, ":", 2)
	lenderSymbol := strings.SplitN(lenderSymbols[0].SymbolValue, ":", 2)
	var illMessage = iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: borrowerSymbol[0],
					},
					AgencyIdValue: borrowerSymbol[1],
				},
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: lenderSymbol[0],
					},
					AgencyIdValue: lenderSymbol[1],
				},
				RequestingAgencyRequestId: pr.ID,
			},
			Action: action,
		},
	}
	w := NewResponseCaptureWriter()
	a.iso18626Handler.HandleRequestingAgencyMessage(ctx, &illMessage, w)
	result.OutgoingMessage = &illMessage
	result.IncomingMessage = w.IllMessage
	return "", nil, &w.StatusCode
}

func (a *PatronRequestActionService) cancelBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionCancel)
	if httpStatus == nil {
		return status, eventResult
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return events.EventStatusProblem, &result
	}
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateCancelPending, &result)
}

func (a *PatronRequestActionService) acceptConditionBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	// TODO Make NCIP calls
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateWillSupply, nil)
}

func (a *PatronRequestActionService) rejectConditionBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	// TODO Make NCIP calls
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateCancelPending, nil)
}

type ResponseCaptureWriter struct {
	IllMessage *iso18626.ISO18626Message
	StatusCode int
}

func NewResponseCaptureWriter() *ResponseCaptureWriter {
	return &ResponseCaptureWriter{
		StatusCode: http.StatusOK,
	}
}
func (rcw *ResponseCaptureWriter) Write(b []byte) (int, error) {
	err := xml.Unmarshal(b, &rcw.IllMessage)
	return 1, err
}
func (rcw *ResponseCaptureWriter) WriteHeader(code int) {
	rcw.StatusCode = code
}
func (rcw *ResponseCaptureWriter) Header() http.Header {
	return http.Header{}
}
