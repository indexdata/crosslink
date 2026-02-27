package prservice

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

const COMP = "pr_action_service"
const (
	ActionOutcomeSuccess = "success"
	ActionOutcomeFailure = "failure"
)

const (
	SideBorrowing pr_db.PatronRequestSide = "borrowing"
	SideLending   pr_db.PatronRequestSide = "lending"
)

const (
	BorrowerStateNew              pr_db.PatronRequestState = "NEW"
	BorrowerStateValidated        pr_db.PatronRequestState = "VALIDATED"
	BorrowerStateSent             pr_db.PatronRequestState = "SENT"
	BorrowerStateSupplierLocated  pr_db.PatronRequestState = "SUPPLIER_LOCATED"
	BorrowerStateConditionPending pr_db.PatronRequestState = "CONDITION_PENDING"
	BorrowerStateWillSupply       pr_db.PatronRequestState = "WILL_SUPPLY"
	BorrowerStateShipped          pr_db.PatronRequestState = "SHIPPED"
	BorrowerStateReceived         pr_db.PatronRequestState = "RECEIVED"
	BorrowerStateCheckedOut       pr_db.PatronRequestState = "CHECKED_OUT"
	BorrowerStateCheckedIn        pr_db.PatronRequestState = "CHECKED_IN"
	BorrowerStateShippedReturned  pr_db.PatronRequestState = "SHIPPED_RETURNED"
	BorrowerStateCancelPending    pr_db.PatronRequestState = "CANCEL_PENDING"
	BorrowerStateCompleted        pr_db.PatronRequestState = "COMPLETED"
	BorrowerStateCancelled        pr_db.PatronRequestState = "CANCELLED"
	BorrowerStateUnfilled         pr_db.PatronRequestState = "UNFILLED"
	LenderStateNew                pr_db.PatronRequestState = "NEW"
	LenderStateValidated          pr_db.PatronRequestState = "VALIDATED"
	LenderStateWillSupply         pr_db.PatronRequestState = "WILL_SUPPLY"
	LenderStateConditionPending   pr_db.PatronRequestState = "CONDITION_PENDING"
	LenderStateConditionAccepted  pr_db.PatronRequestState = "CONDITION_ACCEPTED"
	LenderStateShipped            pr_db.PatronRequestState = "SHIPPED"
	LenderStateShippedReturn      pr_db.PatronRequestState = "SHIPPED_RETURN"
	LenderStateCancelRequested    pr_db.PatronRequestState = "CANCEL_REQUESTED"
	LenderStateCompleted          pr_db.PatronRequestState = "COMPLETED"
	LenderStateCancelled          pr_db.PatronRequestState = "CANCELLED"
	LenderStateUnfilled           pr_db.PatronRequestState = "UNFILLED"
)

const (
	BorrowerActionValidate        pr_db.PatronRequestAction = "validate"
	BorrowerActionSendRequest     pr_db.PatronRequestAction = "send-request"
	BorrowerActionCancelRequest   pr_db.PatronRequestAction = "cancel-request"
	BorrowerActionAcceptCondition pr_db.PatronRequestAction = "accept-condition"
	BorrowerActionRejectCondition pr_db.PatronRequestAction = "reject-condition"
	BorrowerActionReceive         pr_db.PatronRequestAction = "receive"
	BorrowerActionCheckOut        pr_db.PatronRequestAction = "check-out"
	BorrowerActionCheckIn         pr_db.PatronRequestAction = "check-in"
	BorrowerActionShipReturn      pr_db.PatronRequestAction = "ship-return"

	LenderActionValidate      pr_db.PatronRequestAction = "validate"
	LenderActionWillSupply    pr_db.PatronRequestAction = "will-supply"
	LenderActionCannotSupply  pr_db.PatronRequestAction = "cannot-supply"
	LenderActionAddCondition  pr_db.PatronRequestAction = "add-condition"
	LenderActionShip          pr_db.PatronRequestAction = "ship"
	LenderActionMarkReceived  pr_db.PatronRequestAction = "mark-received"
	LenderActionMarkCancelled pr_db.PatronRequestAction = "mark-cancelled"
)

type PatronRequestActionService struct {
	prRepo               pr_db.PrRepo
	eventBus             events.EventBus
	iso18626Handler      handler.Iso18626HandlerInterface
	lmsCreator           lms.LmsCreator
	actionMappingService ActionMappingService
}

type actionExecutionResult struct {
	status    events.EventStatus
	result    *events.EventResult
	outcome   string
	pr        pr_db.PatronRequest
	persistPr bool
}

func CreatePatronRequestActionService(prRepo pr_db.PrRepo, eventBus events.EventBus, iso18626Handler handler.Iso18626HandlerInterface, lmsCreator lms.LmsCreator) *PatronRequestActionService {
	return &PatronRequestActionService{
		prRepo:               prRepo,
		eventBus:             eventBus,
		iso18626Handler:      iso18626Handler,
		lmsCreator:           lmsCreator,
		actionMappingService: ActionMappingService{},
	}
}

func (a *PatronRequestActionService) InvokeAction(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(COMP))
	_, _ = a.processInvokeActionTask(ctx, event)
}

func (a *PatronRequestActionService) ProcessInvokeActionTask(ctx common.ExtendedContext, event events.Event) (events.Event, error) {
	// Invoke-action tasks are currently processed inline by their callers.
	return a.processInvokeActionTask(ctx, event)
}

func (a *PatronRequestActionService) processInvokeActionTask(ctx common.ExtendedContext, event events.Event) (events.Event, error) {
	return a.eventBus.ProcessTask(ctx, event, a.handleInvokeAction)
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
	actionMapping := a.actionMappingService.GetActionMapping(pr)
	if !actionMapping.IsActionSupported(pr, action) {
		return events.LogErrorAndReturnResult(ctx, "state "+string(pr.State)+" does not support action "+string(action), errors.New("invalid action"))
	}
	if a.lmsCreator == nil {
		return events.LogErrorAndReturnResult(ctx, "LMS creator not configured", nil)
	}
	var illRequest iso18626.Request
	err = json.Unmarshal(pr.IllRequest, &illRequest)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to parse ILL request", err)
	}
	switch pr.Side {
	case SideBorrowing:
		execResult := a.handleBorrowingAction(ctx, action, pr, illRequest)
		return a.finalizeActionExecution(ctx, event, actionMapping, action, pr, execResult)
	case SideLending:
		execResult := a.handleLenderAction(ctx, action, pr, illRequest, event.EventData.CustomData)
		return a.finalizeActionExecution(ctx, event, actionMapping, action, pr, execResult)
	default:
		return events.LogErrorAndReturnResult(ctx, "side "+string(pr.Side)+" is not supported", errors.New("invalid side"))
	}
}

func (a *PatronRequestActionService) finalizeActionExecution(ctx common.ExtendedContext, event events.Event, actionMapping *ActionMapping, action pr_db.PatronRequestAction, currentPr pr_db.PatronRequest, execResult actionExecutionResult) (events.EventStatus, *events.EventResult) {
	updatedPr := execResult.pr
	stateChanged := false

	if transitionState, ok := actionMapping.GetActionTransition(currentPr, action, execResult.outcome); ok && transitionState != updatedPr.State {
		updatedPr.State = transitionState
		execResult.persistPr = true
		stateChanged = true
	}

	if execResult.persistPr {
		var err error
		updatedPr, err = a.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(updatedPr))
		if err != nil {
			return events.LogErrorAndReturnResult(ctx, "failed to update patron request", err)
		}
	}

	if stateChanged {
		err := a.RunAutoActionsOnStateEntry(ctx, updatedPr, &event.ID)
		if err != nil {
			return events.LogErrorAndReturnResult(ctx, "failed to execute auto action", err)
		}
	}

	return execResult.status, execResult.result
}

func (a *PatronRequestActionService) RunAutoActionsOnStateEntry(ctx common.ExtendedContext, pr pr_db.PatronRequest, parentEventID *string) error {
	autoActions := a.actionMappingService.GetActionMapping(pr).GetAutoActionsForState(pr)
	if len(autoActions) == 0 {
		return nil
	}

	currentState := pr.State
	for _, action := range autoActions {
		data := events.EventData{CommonEventData: events.CommonEventData{Action: &action}}
		eventID, err := a.eventBus.CreateTask(pr.ID, events.EventNameInvokeAction, data, events.EventDomainPatronRequest, parentEventID)
		if err != nil {
			return err
		}

		autoEvent := events.Event{
			ID:              eventID,
			PatronRequestID: pr.ID,
			EventData:       data,
		}
		// Auto actions execute inline here to preserve synchronous state-transition semantics.
		completedEvent, err := a.processInvokeActionTask(ctx, autoEvent)
		if err != nil {
			return err
		}
		if completedEvent.EventStatus != events.EventStatusSuccess {
			return fmt.Errorf("auto action %s failed with status %s%s", action, completedEvent.EventStatus, autoActionErrorSuffix(completedEvent))
		}

		updatedPr, err := a.prRepo.GetPatronRequestById(ctx, pr.ID)
		if err != nil {
			return err
		}
		stateChanged := updatedPr.State != currentState
		if stateChanged {
			return nil
		}
	}

	return nil
}

func autoActionErrorSuffix(event events.Event) string {
	if event.ResultData.EventError != nil && event.ResultData.EventError.Message != "" {
		return ": " + event.ResultData.EventError.Message
	}
	if event.ResultData.Problem != nil && event.ResultData.Problem.Details != "" {
		return ": " + event.ResultData.Problem.Details
	}
	return ""
}

func (a *PatronRequestActionService) handleBorrowingAction(ctx common.ExtendedContext, action pr_db.PatronRequestAction, pr pr_db.PatronRequest, illRequest iso18626.Request) actionExecutionResult {
	if !pr.RequesterSymbol.Valid {
		status, result := events.LogErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	lmsAdapter, err := a.lmsCreator.GetAdapter(ctx, pr.RequesterSymbol.String)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	lmsAdapter.SetLogFunc(func(outgoing []byte, incoming []byte, err error) {
		status := events.EventStatusSuccess
		if err != nil {
			status = events.EventStatusError
		}
		var customData = make(map[string]any)
		customData["lmsOutgoingMessage"] = string(outgoing)
		customData["lmsIncomingMessage"] = string(incoming)
		eventData := events.EventData{CustomData: customData}
		_, createErr := a.eventBus.CreateNotice(pr.ID, events.EventNameLmsRequesterMessage, eventData, status, events.EventDomainPatronRequest)
		if createErr != nil {
			ctx.Logger().Error("failed to create LMS log event", "error", createErr)
		}
	})
	switch action {
	case BorrowerActionValidate:
		return a.validateBorrowingRequest(ctx, pr, lmsAdapter)
	case BorrowerActionSendRequest:
		return a.sendBorrowingRequest(ctx, pr, illRequest)
	case BorrowerActionReceive:
		return a.receiveBorrowingRequest(ctx, pr, lmsAdapter, illRequest)
	case BorrowerActionCheckOut:
		return a.checkoutBorrowingRequest(ctx, pr, lmsAdapter, illRequest)
	case BorrowerActionCheckIn:
		return a.checkinBorrowingRequest(ctx, pr, lmsAdapter, illRequest)
	case BorrowerActionShipReturn:
		return a.shipReturnBorrowingRequest(ctx, pr, lmsAdapter, illRequest)
	case BorrowerActionCancelRequest:
		return a.cancelBorrowingRequest(ctx, pr)
	case BorrowerActionAcceptCondition:
		return a.acceptConditionBorrowingRequest(ctx, pr)
	case BorrowerActionRejectCondition:
		return a.rejectConditionBorrowingRequest(ctx, pr)
	default:
		status, result := events.LogErrorAndReturnResult(ctx, "borrower action "+string(action)+" is not implemented yet", errors.New("invalid action"))
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
}

func (a *PatronRequestActionService) handleLenderAction(ctx common.ExtendedContext, action pr_db.PatronRequestAction, pr pr_db.PatronRequest, illRequest iso18626.Request, actionParams map[string]interface{}) actionExecutionResult {
	if !pr.SupplierSymbol.Valid {
		status, result := events.LogErrorAndReturnResult(ctx, "missing supplier symbol", nil)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	lms, err := a.lmsCreator.GetAdapter(ctx, pr.SupplierSymbol.String)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	lms.SetLogFunc(func(outgoing []byte, incoming []byte, err error) {
		status := events.EventStatusSuccess
		if err != nil {
			status = events.EventStatusError
		}
		var customData = make(map[string]any)
		customData["lmsOutgoingMessage"] = string(outgoing)
		customData["lmsIncomingMessage"] = string(incoming)
		eventData := events.EventData{CustomData: customData}
		_, createErr := a.eventBus.CreateNotice(pr.ID, events.EventNameLmsSupplierMessage, eventData, status, events.EventDomainPatronRequest)
		if createErr != nil {
			ctx.Logger().Error("failed to create LMS log event", "error", err)
		}
	})
	switch action {
	case LenderActionValidate:
		return a.validateLenderRequest(ctx, pr, lms)
	case LenderActionWillSupply:
		return a.willSupplyLenderRequest(ctx, pr, lms, illRequest)
	case LenderActionCannotSupply:
		return a.cannotSupplyLenderRequest(ctx, pr)
	case LenderActionAddCondition:
		return a.addConditionsLenderRequest(ctx, pr, actionParams)
	case LenderActionShip:
		return a.shipLenderRequest(ctx, pr, lms, illRequest)
	case LenderActionMarkReceived:
		return a.markReceivedLenderRequest(ctx, pr, lms, illRequest)
	case LenderActionMarkCancelled:
		return a.markCancelledLenderRequest(ctx, pr)
	default:
		status, result := events.LogErrorAndReturnResult(ctx, "lender action "+string(action)+" is not implemented yet", errors.New("invalid action"))
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
}

func (a *PatronRequestActionService) validateBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter) actionExecutionResult {
	patron := ""
	if pr.Patron.Valid {
		patron = pr.Patron.String
	}
	userId, err := lmsAdapter.LookupUser(patron)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS LookupUser failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	// change patron to canonical user id
	// perhaps it would be better to have both original and canonical id stored?
	pr.Patron = pgtype.Text{String: userId, Valid: true}
	return actionExecutionResult{status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, pr: pr, persistPr: true}
}

func (a *PatronRequestActionService) sendBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, request iso18626.Request) actionExecutionResult {
	result := events.EventResult{}
	// pr.RequesterSymbol is validated earlier in handleBorrowingAction
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	if len(requesterSymbol) != 2 {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "invalid requester symbol", nil)
		return actionExecutionResult{status: status, result: eventResult, outcome: ActionOutcomeFailure, pr: pr}
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
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, outcome: ActionOutcomeSuccess, pr: pr}
}

func callNumberFromIllRequest(illRequest iso18626.Request) string {
	callNumber := ""
	if len(illRequest.SupplierInfo) > 0 {
		callNumber = illRequest.SupplierInfo[0].CallNumber
	}
	return callNumber
}

func isbnFromIllRequest(illRequest iso18626.Request) string {
	isbn := ""
	if len(illRequest.BibliographicInfo.BibliographicItemId) > 0 &&
		illRequest.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifierCode.Text == "ISBN" {
		isbn = illRequest.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifier
	}
	return isbn
}

func (a *PatronRequestActionService) receiveBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	patron := ""
	if pr.Patron.Valid {
		patron = pr.Patron.String
	}
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	requestId := illRequest.Header.RequestingAgencyRequestId
	author := illRequest.BibliographicInfo.Author
	title := illRequest.BibliographicInfo.Title
	isbn := isbnFromIllRequest(illRequest)
	callNumber := callNumberFromIllRequest(illRequest)
	pickupLocation := lmsAdapter.RequesterPickupLocation()
	requestedAction := "Hold For Pickup"
	err := lmsAdapter.AcceptItem(itemId, requestId, patron, author, title, isbn, callNumber, pickupLocation, requestedAction)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS AcceptItem failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionReceived)
	if httpStatus == nil {
		return actionExecutionResult{status: status, result: eventResult, outcome: ActionOutcomeFailure, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, outcome: ActionOutcomeSuccess, pr: pr}
}

func (a *PatronRequestActionService) checkoutBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	patron := ""
	if pr.Patron.Valid {
		patron = pr.Patron.String
	}
	requestId := illRequest.Header.RequestingAgencyRequestId
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	borrowerBarcode := patron
	err := lmsAdapter.CheckOutItem(requestId, itemId, borrowerBarcode, "externalReferenceValue")
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS CheckOutItem failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, pr: pr}
}

func (a *PatronRequestActionService) checkinBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	err := lmsAdapter.CheckInItem(itemId)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS CheckInItem failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, pr: pr}
}

func (a *PatronRequestActionService) shipReturnBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	err := lmsAdapter.DeleteItem(itemId)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS DeleteItem failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionShippedReturn)
	if httpStatus == nil {
		return actionExecutionResult{status: status, result: eventResult, outcome: ActionOutcomeFailure, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, outcome: ActionOutcomeSuccess, pr: pr}
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

func (a *PatronRequestActionService) cancelBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionCancel)
	if httpStatus == nil {
		return actionExecutionResult{status: status, result: eventResult, outcome: ActionOutcomeFailure, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, outcome: ActionOutcomeSuccess, pr: pr}
}

func (a *PatronRequestActionService) acceptConditionBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	return actionExecutionResult{status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, pr: pr}
}

func (a *PatronRequestActionService) rejectConditionBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	return actionExecutionResult{status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, pr: pr}
}

func (a *PatronRequestActionService) validateLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lms lms.LmsAdapter) actionExecutionResult {
	institutionalPatron := lms.InstitutionalPatron(pr.RequesterSymbol.String)
	_, err := lms.LookupUser(institutionalPatron)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS LookupUser failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, outcome: ActionOutcomeSuccess, pr: pr}
}

func (a *PatronRequestActionService) willSupplyLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	requestId := illRequest.Header.RequestingAgencyRequestId
	userId := lmsAdapter.InstitutionalPatron(pr.RequesterSymbol.String)
	pickupLocation := lmsAdapter.SupplierPickupLocation()
	itemLocation := lmsAdapter.ItemLocation()
	err := lmsAdapter.RequestItem(requestId, itemId, userId, pickupLocation, itemLocation)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS RequestItem failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageNotification}, iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) cannotSupplyLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusUnfilled})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) addConditionsLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, actionParams map[string]interface{}) actionExecutionResult {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result,
		iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageNotification,
			Note:             RESHARE_ADD_LOAN_CONDITION, // TODO add action params
		},
		iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) shipLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	requestId := illRequest.Header.RequestingAgencyRequestId
	userId := lmsAdapter.InstitutionalPatron(pr.RequesterSymbol.String)
	// TODO set these values properly
	externalReferenceValue := ""
	err := lmsAdapter.CheckOutItem(requestId, itemId, userId, externalReferenceValue)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS CheckOutItem failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) markReceivedLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	err := lmsAdapter.CheckInItem(itemId)
	if err != nil {
		status, result := events.LogErrorAndReturnResult(ctx, "LMS CheckInItem failed", err)
		return actionExecutionResult{status: status, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusLoanCompleted})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) markCancelledLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) sendSupplyingAgencyMessage(ctx common.ExtendedContext, pr pr_db.PatronRequest, result *events.EventResult, messageInfo iso18626.MessageInfo, statusInfo iso18626.StatusInfo) (events.EventStatus, *events.EventResult, *int) {
	if !pr.RequesterSymbol.Valid {
		status, eventResult := events.LogErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return status, eventResult, nil
	}
	// pr.SupplierSymbol is validated earlier in handleLenderAction
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

func (a *PatronRequestActionService) checkSupplyingResponse(status events.EventStatus, eventResult *events.EventResult, result *events.EventResult, httpStatus *int, pr pr_db.PatronRequest) actionExecutionResult {
	if httpStatus == nil {
		return actionExecutionResult{status: status, result: eventResult, outcome: ActionOutcomeFailure, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.SupplyingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		return actionExecutionResult{status: events.EventStatusProblem, result: result, outcome: ActionOutcomeFailure, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: nil, outcome: ActionOutcomeSuccess, pr: pr}
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
