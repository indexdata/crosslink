package prservice

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/lms"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/shim"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

const COMP = "pr_action_service"

type PatronRequestActionService struct {
	prRepo               pr_db.PrRepo
	eventBus             events.EventBus
	iso18626Handler      handler.Iso18626HandlerInterface
	lmsCreator           lms.LmsCreator
	actionMappingService ActionMappingService
}

type actionExecutionResult struct {
	status events.EventStatus
	result *events.EventResult
	pr     pr_db.PatronRequest
}

func CreatePatronRequestActionService(prRepo pr_db.PrRepo, eventBus events.EventBus, iso18626Handler handler.Iso18626HandlerInterface, lmsCreator lms.LmsCreator) *PatronRequestActionService {
	return &PatronRequestActionService{
		prRepo:               prRepo,
		eventBus:             eventBus,
		iso18626Handler:      iso18626Handler,
		lmsCreator:           lmsCreator,
		actionMappingService: ActionMappingService{SMService: &StateModelService{}},
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

func (a *PatronRequestActionService) logErrorAndReturnResult(ctx common.ExtendedContext, message string, err error) (events.EventStatus, *events.EventResult) {
	status, result := events.LogErrorAndReturnResult(ctx, message, err)
	result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
	return status, result
}

func (a *PatronRequestActionService) handleInvokeAction(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	if event.EventData.Action == nil {
		return a.logErrorAndReturnResult(ctx, "action not specified", errors.New("action not specified"))
	}
	action := *event.EventData.Action
	pr, err := a.prRepo.GetPatronRequestById(ctx, event.PatronRequestID)
	if err != nil {
		return a.logErrorAndReturnResult(ctx, "failed to read patron request", err)
	}
	actionMapping, err := a.actionMappingService.GetActionMapping(pr)
	if err != nil {
		return a.logErrorAndReturnResult(ctx, "failed to load state model", err)
	}
	if !actionMapping.IsActionSupported(pr, action) {
		return a.logErrorAndReturnResult(ctx, "state "+string(pr.State)+" does not support action "+string(action), errors.New("invalid action"))
	}
	if a.lmsCreator == nil {
		return a.logErrorAndReturnResult(ctx, "LMS creator not configured", nil)
	}
	illRequest := pr.IllRequest
	switch pr.Side {
	case SideBorrowing:
		execResult := a.handleBorrowingAction(ctx, action, pr, illRequest, &event.ID)
		return a.finalizeActionExecution(ctx, event, actionMapping, action, pr, execResult)
	case SideLending:
		execResult := a.handleLenderAction(ctx, action, pr, illRequest, event.EventData.CustomData, &event.ID)
		return a.finalizeActionExecution(ctx, event, actionMapping, action, pr, execResult)
	default:
		return a.logErrorAndReturnResult(ctx, "side "+string(pr.Side)+" is not supported", errors.New("invalid side"))
	}
}

func (a *PatronRequestActionService) finalizeActionExecution(ctx common.ExtendedContext, event events.Event, actionMapping *ActionMapping, action pr_db.PatronRequestAction, currentPr pr_db.PatronRequest, execResult actionExecutionResult) (events.EventStatus, *events.EventResult) {
	if execResult.result == nil {
		execResult.result = &events.EventResult{}
	}
	if execResult.result.ActionResult == nil {
		execResult.result.ActionResult = &events.ActionResult{}
		outcome := ActionOutcomeSuccess
		execResult.result.ActionResult.Outcome = outcome
	}
	outcome := execResult.result.ActionResult.Outcome
	updatedPr := execResult.pr
	updatedPr.LastAction = getDbText(string(action))
	updatedPr.LastActionOutcome = getDbText(outcome)
	updatedPr.LastActionResult = getDbText(string(execResult.status))
	if outcome == ActionOutcomeFailure {
		updatedPr.NeedsAttention = true
	}
	stateChanged := false
	if transitionState, ok := actionMapping.GetActionTransition(currentPr, action, outcome); ok && transitionState != updatedPr.State {
		updatedPr.State = transitionState
		toState := string(transitionState)
		execResult.result.ActionResult.ToState = &toState
		stateChanged = true
	}

	var err error
	updatedPr, err = a.prRepo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(updatedPr))
	if err != nil {
		return a.logErrorAndReturnResult(ctx, "failed to update patron request", err)
	}

	if stateChanged {
		err := a.RunAutoActionsOnStateEntry(ctx, updatedPr, &event.ID)
		if err != nil {
			if !updatedPr.NeedsAttention {
				a.setNeedsAttention(ctx, updatedPr)
			}
			return a.logErrorAndReturnResult(ctx, "failed to execute auto action", err)
		}
	}

	return execResult.status, execResult.result
}

func (a *PatronRequestActionService) RunAutoActionsOnStateEntry(ctx common.ExtendedContext, pr pr_db.PatronRequest, parentEventID *string) error {
	actionMapping, err := a.actionMappingService.GetActionMapping(pr)
	if err != nil {
		return err
	}
	autoActions := actionMapping.GetAutoActionsForState(pr)
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

func (a *PatronRequestActionService) handleBorrowingAction(ctx common.ExtendedContext, action pr_db.PatronRequestAction, pr pr_db.PatronRequest, illRequest iso18626.Request, eventID *string) actionExecutionResult {
	if !pr.RequesterSymbol.Valid {
		status, result := a.logErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	lmsAdapter, err := a.lmsCreator.GetAdapter(ctx, pr.RequesterSymbol.String)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	lmsAdapter.SetLogFunc(func(outgoing map[string]any, incoming map[string]any, err error) {
		status := events.EventStatusSuccess
		if err != nil {
			status = events.EventStatusError
		}
		var customData = make(map[string]any)
		customData["lmsOutgoingMessage"] = outgoing
		customData["lmsIncomingMessage"] = incoming
		eventData := events.EventData{CustomData: customData}
		_, createErr := a.eventBus.CreateNoticeWithParent(pr.ID, events.EventNameLmsRequesterMessage, eventData, status, events.EventDomainPatronRequest, eventID)
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
		status, result := a.logErrorAndReturnResult(ctx, "borrower action "+string(action)+" is not implemented yet", errors.New("invalid action"))
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
}

func (a *PatronRequestActionService) handleLenderAction(ctx common.ExtendedContext, action pr_db.PatronRequestAction, pr pr_db.PatronRequest, illRequest iso18626.Request, actionParams map[string]interface{}, eventID *string) actionExecutionResult {
	if !pr.SupplierSymbol.Valid {
		status, result := a.logErrorAndReturnResult(ctx, "missing supplier symbol", nil)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	lms, err := a.lmsCreator.GetAdapter(ctx, pr.SupplierSymbol.String)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	lms.SetLogFunc(func(outgoing map[string]any, incoming map[string]any, err error) {
		status := events.EventStatusSuccess
		if err != nil {
			status = events.EventStatusError
		}
		var customData = make(map[string]any)
		customData["lmsOutgoingMessage"] = outgoing
		customData["lmsIncomingMessage"] = incoming
		eventData := events.EventData{CustomData: customData}
		_, createErr := a.eventBus.CreateNoticeWithParent(pr.ID, events.EventNameLmsSupplierMessage, eventData, status, events.EventDomainPatronRequest, eventID)
		if createErr != nil {
			ctx.Logger().Error("failed to create LMS log event", "error", createErr)
		}
	})
	switch action {
	case LenderActionValidate:
		return a.validateLenderRequest(ctx, pr, lms)
	case LenderActionWillSupply:
		return a.willSupplyLenderRequest(ctx, pr, lms, illRequest)
	case LenderActionRejectCancel:
		return a.rejectCancelLenderRequest(ctx, pr)
	case LenderActionCannotSupply:
		return a.cannotSupplyLenderRequest(ctx, pr)
	case LenderActionAddCondition:
		return a.addConditionsLenderRequest(ctx, pr, actionParams)
	case LenderActionShip:
		return a.shipLenderRequest(ctx, pr, lms, illRequest)
	case LenderActionMarkReceived:
		return a.markReceivedLenderRequest(ctx, pr, lms, illRequest)
	case LenderActionAcceptCancel:
		return a.acceptCancelLenderRequest(ctx, pr)
	default:
		status, result := a.logErrorAndReturnResult(ctx, "lender action "+string(action)+" is not implemented yet", errors.New("invalid action"))
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
}

func (a *PatronRequestActionService) validateBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter) actionExecutionResult {
	patron := ""
	if pr.Patron.Valid {
		patron = pr.Patron.String
	}
	userId, err := lmsAdapter.LookupUser(patron)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "LMS LookupUser failed", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	// change patron to canonical user id
	// perhaps it would be better to have both original and canonical id stored?
	pr.Patron = pgtype.Text{String: userId, Valid: true}
	return actionExecutionResult{status: events.EventStatusSuccess, pr: pr}
}

func (a *PatronRequestActionService) sendBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, request iso18626.Request) actionExecutionResult {
	result := events.EventResult{}
	// pr.RequesterSymbol is validated earlier in handleBorrowingAction
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	if len(requesterSymbol) != 2 {
		status, eventResult := a.logErrorAndReturnResult(ctx, "invalid requester symbol", nil)
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}

	illRequest, err := deepCopyISO18626Request(request)
	if err != nil {
		status, eventResult := a.logErrorAndReturnResult(ctx, "failed to clone outgoing ISO18626 request", err)
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	illRequest.Header.RequestingAgencyId = iso18626.TypeAgencyId{
		AgencyIdType: iso18626.TypeSchemeValuePair{
			Text: requesterSymbol[0],
		},
		AgencyIdValue: requesterSymbol[1],
	}
	illRequest.Header.RequestingAgencyRequestId = pr.ID
	if illRequest.PatronInfo == nil {
		illRequest.PatronInfo = &iso18626.PatronInfo{}
	}
	illRequest.PatronInfo.PatronId = pr.Patron.String

	var illMessage = iso18626.ISO18626Message{
		Request: &illRequest,
	}
	w := NewResponseCaptureWriter()
	a.iso18626Handler.HandleRequest(ctx, &illMessage, w)
	result.OutgoingMessage = &illMessage
	result.IncomingMessage = w.IllMessage
	if w.StatusCode != http.StatusOK || w.IllMessage == nil || w.IllMessage.RequestConfirmation == nil ||
		w.IllMessage.RequestConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, pr: pr}
}

func deepCopyISO18626Request(request iso18626.Request) (iso18626.Request, error) {
	requestJSON, err := json.Marshal(request)
	if err != nil {
		return iso18626.Request{}, err
	}
	var clone iso18626.Request
	if err = json.Unmarshal(requestJSON, &clone); err != nil {
		return iso18626.Request{}, err
	}
	return clone, nil
}

func (a *PatronRequestActionService) receiveBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	patron := ""
	if pr.Patron.Valid {
		patron = pr.Patron.String
	}
	items, err := a.getItems(ctx, pr)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "receiveBorrowingRequest failed to get items by PR ID", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	for _, item := range items {
		callNumber := ""
		if item.CallNumber.Valid {
			callNumber = item.CallNumber.String
		}
		title := ""
		if item.Title.Valid {
			title = item.Title.String
		}
		itemId := item.Barcode // requester bar code
		author := pr.IllRequest.BibliographicInfo.Author
		isbn := ""
		pickupLocation := lmsAdapter.RequesterPickupLocation()
		requestedAction := "Hold For Pickup"
		err = lmsAdapter.AcceptItem(itemId, pr.ID, patron, author, title, isbn, callNumber, pickupLocation, requestedAction)
		if err != nil {
			status, result := a.logErrorAndReturnResult(ctx, "LMS AcceptItem failed", err)
			return actionExecutionResult{status: status, result: result, pr: pr}
		}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionReceived, "")
	if httpStatus == nil {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, pr: pr}
}

func (a *PatronRequestActionService) checkoutBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	patron := ""
	if pr.Patron.Valid {
		patron = pr.Patron.String
	}
	items, err := a.getItems(ctx, pr)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "checkoutBorrowingRequest failed to get items by PR ID", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	for _, item := range items {
		itemId := item.Barcode
		borrowerBarcode := patron
		_, err = lmsAdapter.CheckOutItem(pr.ID, itemId, borrowerBarcode, "externalReferenceValue")
		if err != nil {
			status, result := a.logErrorAndReturnResult(ctx, "LMS CheckOutItem failed", err)
			return actionExecutionResult{status: status, result: result, pr: pr}
		}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, pr: pr}
}

func (a *PatronRequestActionService) checkinBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	items, err := a.getItems(ctx, pr)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "checkinBorrowingRequest failed to get items by PR ID", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	for _, item := range items {
		itemId := item.Barcode
		err = lmsAdapter.CheckInItem(itemId)
		if err != nil {
			status, result := a.logErrorAndReturnResult(ctx, "LMS CheckInItem failed", err)
			return actionExecutionResult{status: status, result: result, pr: pr}
		}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, pr: pr}
}

func (a *PatronRequestActionService) shipReturnBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	items, err := a.getItems(ctx, pr)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "shipReturnBorrowingRequest failed to get items by PR ID", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	for _, item := range items {
		itemId := item.Barcode
		err = lmsAdapter.DeleteItem(itemId)
		if err != nil {
			status, result := a.logErrorAndReturnResult(ctx, "LMS DeleteItem failed", err)
			return actionExecutionResult{status: status, result: result, pr: pr}
		}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionShippedReturn, "")
	if httpStatus == nil {
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, pr: pr}
}

func (a *PatronRequestActionService) sendRequestingAgencyMessage(ctx common.ExtendedContext, pr pr_db.PatronRequest, result *events.EventResult, action iso18626.TypeAction, note string) (events.EventStatus, *events.EventResult, *int) {
	if !pr.RequesterSymbol.Valid {
		status, eventResult := a.logErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return status, eventResult, nil
	}
	if !pr.SupplierSymbol.Valid {
		status, eventResult := a.logErrorAndReturnResult(ctx, "missing supplier symbol", nil)
		return status, eventResult, nil
	}
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	if len(requesterSymbol) != 2 {
		status, eventResult := a.logErrorAndReturnResult(ctx, "invalid requester symbol", nil)
		return status, eventResult, nil
	}
	supplierSymbol := strings.SplitN(pr.SupplierSymbol.String, ":", 2)
	if len(supplierSymbol) != 2 {
		status, eventResult := a.logErrorAndReturnResult(ctx, "invalid supplier symbol", nil)
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
			Note:   note,
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
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionCancel, "")
	if httpStatus == nil {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, pr: pr}
}

func (a *PatronRequestActionService) acceptConditionBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionNotification, shim.RESHARE_LOAN_CONDITION_AGREE)
	if httpStatus == nil {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, pr: pr}
}

func (a *PatronRequestActionService) rejectConditionBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendRequestingAgencyMessage(ctx, pr, &result, iso18626.TypeActionCancel, shim.RESHARE_LOAN_CONDITION_REJECT)
	if httpStatus == nil {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.RequestingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, pr: pr}
}

func (a *PatronRequestActionService) validateLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lms lms.LmsAdapter) actionExecutionResult {
	institutionalPatron := lms.InstitutionalPatron(pr.RequesterSymbol.String)
	_, err := lms.LookupUser(institutionalPatron)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "LMS LookupUser failed", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, pr: pr}
}

func (a *PatronRequestActionService) willSupplyLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	requestId := illRequest.Header.RequestingAgencyRequestId
	userId := lmsAdapter.InstitutionalPatron(pr.RequesterSymbol.String)
	pickupLocation := lmsAdapter.SupplierPickupLocation()
	itemLocation := lmsAdapter.ItemLocation()
	itemBarcode, callNumber, title, err := lmsAdapter.RequestItem(requestId, itemId, userId, pickupLocation, itemLocation)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "LMS RequestItem failed", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	if title == "" {
		title = illRequest.BibliographicInfo.Title
	}
	_, err = a.prRepo.SaveItem(ctx, pr_db.SaveItemParams{
		ID:         uuid.NewString(),
		CreatedAt:  pgtype.Timestamp{Valid: true, Time: time.Now()},
		PrID:       pr.ID,
		ItemID:     getDbText(itemId),
		Title:      getDbTextPtr(&title),
		CallNumber: getDbTextPtr(&callNumber),
		Barcode:    itemBarcode,
	})
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "failed to save item", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply})
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
			Note:             shim.RESHARE_ADD_LOAN_CONDITION, // TODO add action params
		},
		iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) shipLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	requestId := illRequest.Header.RequestingAgencyRequestId
	userId := lmsAdapter.InstitutionalPatron(pr.RequesterSymbol.String)
	externalReferenceValue := ""

	items, err := a.getItems(ctx, pr)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "no items for shipping in the request", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	for i := range items {
		item := &items[i]
		title, err := lmsAdapter.CheckOutItem(requestId, item.Barcode, userId, externalReferenceValue)
		if err != nil {
			status, result := a.logErrorAndReturnResult(ctx, "LMS CheckOutItem failed", err)
			return actionExecutionResult{status: status, result: result, pr: pr}
		}
		if title != "" {
			item.Title = getDbText(title)
			_, err = a.prRepo.SaveItem(ctx, pr_db.SaveItemParams{
				ID:         item.ID,
				CreatedAt:  item.CreatedAt,
				PrID:       item.PrID,
				ItemID:     item.ItemID,
				Title:      item.Title,
				CallNumber: item.CallNumber,
				Barcode:    item.Barcode,
			})
			if err != nil {
				status, result := a.logErrorAndReturnResult(ctx, "failed to save item", err)
				return actionExecutionResult{status: status, result: result, pr: pr}
			}
		}
	}
	note := encodeItemsNote(items)
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result,
		iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange, Note: note},
		iso18626.StatusInfo{Status: iso18626.TypeStatusLoaned})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func encodeItemsNote(items []pr_db.Item) string {
	var list [][]string
	for _, item := range items {
		title := ""
		if item.Title.Valid {
			title = item.Title.String
		}
		callnumber := ""
		if item.CallNumber.Valid {
			callnumber = item.CallNumber.String
		}
		list = append(list, []string{item.Barcode, callnumber, title})
	}
	return common.PackItemsNote(list)
}

func (a *PatronRequestActionService) markReceivedLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, lmsAdapter lms.LmsAdapter, illRequest iso18626.Request) actionExecutionResult {
	items, err := a.getItems(ctx, pr)
	if err != nil {
		status, result := a.logErrorAndReturnResult(ctx, "no items for check-in in the request", err)
		return actionExecutionResult{status: status, result: result, pr: pr}
	}
	for _, item := range items {
		err = lmsAdapter.CheckInItem(item.Barcode)
		if err != nil {
			status, result := a.logErrorAndReturnResult(ctx, "LMS CheckInItem failed", err)
			return actionExecutionResult{status: status, result: result, pr: pr}
		}
	}
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result, iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange}, iso18626.StatusInfo{Status: iso18626.TypeStatusLoanCompleted})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) rejectCancelLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	no := iso18626.TypeYesNoN
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result,
		iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
			AnswerYesNo:      &no,
		},
		iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) acceptCancelLenderRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) actionExecutionResult {
	yes := iso18626.TypeYesNoY
	result := events.EventResult{}
	status, eventResult, httpStatus := a.sendSupplyingAgencyMessage(ctx, pr, &result,
		iso18626.MessageInfo{
			ReasonForMessage: iso18626.TypeReasonForMessageCancelResponse,
			AnswerYesNo:      &yes,
		},
		iso18626.StatusInfo{Status: iso18626.TypeStatusCancelled})
	return a.checkSupplyingResponse(status, eventResult, &result, httpStatus, pr)
}

func (a *PatronRequestActionService) sendSupplyingAgencyMessage(ctx common.ExtendedContext, pr pr_db.PatronRequest, result *events.EventResult, messageInfo iso18626.MessageInfo, statusInfo iso18626.StatusInfo) (events.EventStatus, *events.EventResult, *int) {
	if !pr.RequesterSymbol.Valid {
		status, eventResult := a.logErrorAndReturnResult(ctx, "missing requester symbol", nil)
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
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	if *httpStatus != http.StatusOK || result.IncomingMessage == nil || result.IncomingMessage.SupplyingAgencyMessageConfirmation == nil ||
		result.IncomingMessage.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, pr: pr}
}

func (a *PatronRequestActionService) setNeedsAttention(ctx common.ExtendedContext, pr pr_db.PatronRequest) {
	err := a.prRepo.WithTxFunc(ctx, func(repo pr_db.PrRepo) error {
		prToUpdate, err := repo.GetPatronRequestByIdForUpdate(ctx, pr.ID)
		if err != nil {
			return err
		}
		if prToUpdate.NeedsAttention {
			return nil
		}
		prToUpdate.NeedsAttention = true
		_, err = repo.UpdatePatronRequest(ctx, pr_db.UpdatePatronRequestParams(prToUpdate))
		return err
	})
	if err != nil {
		ctx.Logger().Error("failed to set needs attention", "pr_id", pr.ID, "error", err)
		return
	}
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

func (a *PatronRequestActionService) getItems(ctx common.ExtendedContext, pr pr_db.PatronRequest) ([]pr_db.Item, error) {
	items, err := a.prRepo.GetItemsByPrId(ctx, pr.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get items: %w", err)
	}
	if len(items) == 0 {
		return nil, errors.New("no items found for patron request")
	}
	return items, nil
}
