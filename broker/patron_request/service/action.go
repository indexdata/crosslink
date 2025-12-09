package prservice

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"net/http"
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

var LenderStateNew = "NEW"
var LenderStateValidated = "VALIDATED"
var LenderStateWillSupply = "WILL_SUPPLY"
var LenderStateConditionPending = "CONDITION_PENDING"
var LenderStateConditionAccepted = "CONDITION_ACCEPTED"
var LenderStateShipped = "SHIPPED"
var LenderStateShippedReturn = "SHIPPED_RETURN"
var LenderStateCancelRequested = "CANCEL_REQUESTED"
var LenderStateCompleted = "COMPLETED"
var LenderStateCancelled = "CANCELLED"
var LenderStateUnfilled = "UNFILLED"

var ActionValidate = "validate"
var ActionSendRequest = "send-request"
var ActionCancelRequest = "cancel-request"
var ActionAcceptCondition = "accept-condition"
var ActionRejectCondition = "reject-condition"
var ActionReceive = "receive"
var ActionCheckOut = "check-out"
var ActionCheckIn = "check-in"
var ActionShipReturn = "ship-return"
var ActionWillSupply = "will-supply"
var ActionCannotSupply = "cannot-supply"
var ActionAddCondition = "add-condition"
var ActionShip = "ship"
var ActionMarkReceived = "mark-received"
var ActionMarkCancelled = "mark-cancelled"

type PatronRequestActionService struct {
	prRepo               pr_db.PrRepo
	illRepo              ill_db.IllRepo
	eventBus             events.EventBus
	iso18626Handler      handler.Iso18626HandlerInterface
	actionMappingService ActionMappingService
}

func CreatePatronRequestActionService(prRepo pr_db.PrRepo, illRepo ill_db.IllRepo, eventBus events.EventBus, iso18626Handler handler.Iso18626HandlerInterface) PatronRequestActionService {
	return PatronRequestActionService{
		prRepo:               prRepo,
		illRepo:              illRepo,
		eventBus:             eventBus,
		iso18626Handler:      iso18626Handler,
		actionMappingService: ActionMappingService{},
	}
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
	if !a.actionMappingService.GetActionMapping(pr).IsActionAvailable(pr, action) {
		return events.LogErrorAndReturnResult(ctx, "state "+pr.State+" does not support action "+action, errors.New("invalid action"))
	}
	switch pr.Side {
	case SideBorrowing:
		return a.handleBorrowingAction(ctx, action, pr)
	case SideLanding:
		return a.handleLenderAction(ctx, action, pr, event.EventData.CustomData)
	default:
		return events.LogErrorAndReturnResult(ctx, "side "+pr.Side+" is not supported", errors.New("invalid side"))
	}
}

func (a *PatronRequestActionService) handleBorrowingAction(ctx common.ExtendedContext, action string, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
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
		return events.LogErrorAndReturnResult(ctx, "borrower action "+action+" is not implemented yet", errors.New("invalid action"))
	}
}

func (a *PatronRequestActionService) handleLenderAction(ctx common.ExtendedContext, action string, pr pr_db.PatronRequest, actionParams map[string]interface{}) (events.EventStatus, *events.EventResult) {
	switch action {
	case ActionValidate:
		return a.validateLenderRequest(ctx, pr)
	case ActionWillSupply:
		return a.willSupplyLenderRequest(ctx, pr)
	case ActionCannotSupply:
		return a.cannotSupplyLenderRequest(ctx, pr)
	case ActionAddCondition:
		return a.addConditionsLenderRequest(ctx, pr, actionParams)
	case ActionShip:
		return a.shipLenderRequest(ctx, pr)
	case ActionMarkReceived:
		return a.markReceivedLenderRequest(ctx, pr)
	case ActionMarkCancelled:
		return a.markCancelledLenderRequest(ctx, pr)
	default:
		return events.LogErrorAndReturnResult(ctx, "lender action "+action+" is not implemented yet", errors.New("invalid action"))
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
func (a *PatronRequestActionService) checkSupplyingResponseAndUpdateState(ctx common.ExtendedContext, pr pr_db.PatronRequest, state string, result *events.EventResult, status events.EventStatus, eventResult *events.EventResult, httpStatus *int) (events.EventStatus, *events.EventResult) {
	if httpStatus == nil {
		return status, eventResult
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.SupplyingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return events.EventStatusProblem, result
	}
	return a.updateStateAndReturnResult(ctx, pr, state, nil)
}

func (a *PatronRequestActionService) validateBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	if !pr.Tenant.Valid {
		return events.LogErrorAndReturnResult(ctx, "missing tenant", nil)
	}
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateValidated, nil)
}

func (a *PatronRequestActionService) sendBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	if !pr.RequesterSymbol.Valid {
		return events.LogErrorAndReturnResult(ctx, "missing requester symbol", nil)
	}
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	if len(requesterSymbol) != 2 {
		return events.LogErrorAndReturnResult(ctx, "invalid requester symbol", nil)
	}

	var request *iso18626.Request
	err := json.Unmarshal(pr.IllRequest, &request)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to parse request", err)
	}
	var illMessage = iso18626.ISO18626Message{
		Request: &iso18626.Request{
			Header: iso18626.Header{
				RequestingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: requesterSymbol[0],
					},
					AgencyIdValue: requesterSymbol[1],
				},
				RequestingAgencyRequestId: pr.ID,
			},
			PatronInfo:        &iso18626.PatronInfo{PatronId: pr.Patron.String},
			BibliographicInfo: request.BibliographicInfo,
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
	if !pr.RequesterSymbol.Valid {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return status, eventResult, nil
	}
	if !pr.SupplierSymbol.Valid {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing supplier symbol", nil)
		return status, eventResult, nil
	}
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	if len(requesterSymbol) != 2 {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "invalid requester symbol", nil)
		return status, eventResult, nil
	}
	supplierSymbol := strings.SplitN(pr.SupplierSymbol.String, ":", 2)
	if len(supplierSymbol) != 2 {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "invalid supplier symbol", nil)
		return status, eventResult, nil
	}
	var illMessage = iso18626.ISO18626Message{
		RequestingAgencyMessage: &iso18626.RequestingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: requesterSymbol[0],
					},
					AgencyIdValue: requesterSymbol[1],
				},
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: supplierSymbol[0],
					},
					AgencyIdValue: supplierSymbol[1],
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

func (a *PatronRequestActionService) validateLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	// TODO do validation

	return a.updateStateAndReturnResult(ctx, pr, LenderStateValidated, nil)
}

func (a *PatronRequestActionService) willSupplyLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageNotification}, iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply})
	return a.checkSupplyingResponseAndUpdateState(ctx, pr, LenderStateWillSupply, &result, status, eventResult, httpStatus)
}

func (a *PatronRequestActionService) cannotSupplyLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusUnfilled})
	return a.checkSupplyingResponseAndUpdateState(ctx, pr, LenderStateUnfilled, &result, status, eventResult, httpStatus)
}

func (a *PatronRequestActionService) addConditionsLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, actionParams map[string]interface{}) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result,
		iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
			Note:             "#ReShareAddLoanCondition#", // TODO add action params
		},
		iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply})
	return a.checkSupplyingResponseAndUpdateState(ctx, pr, LenderStateConditionPending, &result, status, eventResult, httpStatus)
}

func (a *PatronRequestActionService) shipLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned})
	return a.checkSupplyingResponseAndUpdateState(ctx, pr, LenderStateShipped, &result, status, eventResult, httpStatus)
}

func (a *PatronRequestActionService) markReceivedLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusLoanCompleted})
	return a.checkSupplyingResponseAndUpdateState(ctx, pr, LenderStateCompleted, &result, status, eventResult, httpStatus)
}

func (a *PatronRequestActionService) markCancelledLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled})
	return a.checkSupplyingResponseAndUpdateState(ctx, pr, LenderStateCancelled, &result, status, eventResult, httpStatus)
}

func (a *PatronRequestActionService) sendSupplyingAgencyMessage(ctx common.ExtendedContext, pr pr_db.PatronRequest, result *events.EventResult, messageInfo iso18626.MessageInfo, statusInfo iso18626.StatusInfo) (events.EventStatus, *events.EventResult, *int) {
	if !pr.RequesterSymbol.Valid {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return status, eventResult, nil
	}
	if !pr.SupplierSymbol.Valid {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing supplier symbol", nil)
		return status, eventResult, nil
	}
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	supplierSymbol := strings.SplitN(pr.SupplierSymbol.String, ":", 2)
	var illMessage = iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			Header: iso18626.Header{
				RequestingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: requesterSymbol[0],
					},
					AgencyIdValue: requesterSymbol[1],
				},
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: supplierSymbol[0],
					},
					AgencyIdValue: supplierSymbol[1],
				},
				RequestingAgencyRequestId: pr.RequesterReqID.String,
				SupplyingAgencyRequestId:  pr.ID,
			},
			MessageInfo: messageInfo,
			StatusInfo:  statusInfo,
		},
	}
	w := NewResponseCaptureWriter()
	a.iso18626Handler.HandleSupplyingAgencyMessage(ctx, &illMessage, w)
	result.OutgoingMessage = &illMessage
	result.IncomingMessage = w.IllMessage
	return "", nil, &w.StatusCode
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
