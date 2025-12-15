package prservice

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/http"
	"slices"
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
	eventBus        events.EventBus
	iso18626Handler handler.Iso18626HandlerInterface
	lmsCreator      lms.LmsCreator
}

func CreatePatronRequestActionService(prRepo pr_db.PrRepo, eventBus events.EventBus, iso18626Handler handler.Iso18626HandlerInterface, lmsCreator lms.LmsCreator) PatronRequestActionService {
	return PatronRequestActionService{
		prRepo:          prRepo,
		eventBus:        eventBus,
		iso18626Handler: iso18626Handler,
		lmsCreator:      lmsCreator,
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
	user := ""
	if pr.Patron.Valid {
		user = pr.Patron.String
	}
	lmsAdapter, err := a.lmsCreator.GetAdapter(ctx, pr.RequesterSymbol)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
	}
	userId, err := lmsAdapter.LookupUser(user)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "LMS LookupUser failed", err)
	}
	// change patron to canonical user id
	// perhaps it would be better to have both original and canonical id stored?
	pr.Patron = pgtype.Text{String: userId, Valid: true}
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
			PatronInfo: &iso18626.PatronInfo{PatronId: pr.Patron.String},
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
	user := ""
	if pr.Patron.Valid {
		user = pr.Patron.String
	}
	lmsAdapter, err := a.lmsCreator.GetAdapter(ctx, pr.RequesterSymbol)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
	}

	var illRequest iso18626.Request
	err = json.Unmarshal(pr.IllRequest, &illRequest)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to unmarshal ILL request", err)
	}
	itemId := illRequest.BibliographicInfo.SupplierUniqueRecordId
	requestId := illRequest.Header.RequestingAgencyRequestId
	author := illRequest.BibliographicInfo.Author
	title := illRequest.BibliographicInfo.Title
	isbn := ""
	if len(illRequest.BibliographicInfo.BibliographicItemId) > 0 &&
		illRequest.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifierCode.Text == "ISBN" {
		isbn = illRequest.BibliographicInfo.BibliographicItemId[0].BibliographicItemIdentifier
	}
	callNumber := ""
	if len(illRequest.SupplierInfo) > 0 {
		callNumber = illRequest.SupplierInfo[0].CallNumber
	}
	pickupLocation := ""
	if len(illRequest.RequestedDeliveryInfo) > 0 {
		address := illRequest.RequestedDeliveryInfo[0].Address
		if address != nil {
			pa := address.PhysicalAddress
			if pa != nil {
				pickupLocation = pa.Line1
				if pa.Line2 != "" {
					pickupLocation += " " + pa.Line2
				}
				if pa.Locality != "" {
					pickupLocation += " " + pa.Locality
				}
				if pa.PostalCode != "" {
					pickupLocation += " " + pa.PostalCode
				}
				if pa.Region != nil {
					pickupLocation += " " + pa.Region.Text
				}
				if pa.Country != nil {
					pickupLocation += " " + pa.Country.Text
				}
			}
		}
	}
	// TODO: get all these parameters from the patron request
	err = lmsAdapter.AcceptItem(itemId, requestId, user, author, title, isbn, callNumber, pickupLocation, "requestedAction")
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "LMS AcceptItem failed", err)
	}
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
	user := ""
	if pr.Patron.Valid {
		user = pr.Patron.String
	}
	lmsAdapter, err := a.lmsCreator.GetAdapter(ctx, pr.RequesterSymbol)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
	}
	err = lmsAdapter.CheckOutItem("requestId", "itemBarcode", user, "externalReferenceValue")
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "LMS CheckOutItem failed", err)
	}
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateCheckedOut, nil)
}

func (a *PatronRequestActionService) checkinBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	lmsAdapter, err := a.lmsCreator.GetAdapter(ctx, pr.RequesterSymbol)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
	}
	item := "" // TODO Get item identifier from the request
	err = lmsAdapter.CheckInItem(item)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "LMS CheckInItem failed", err)
	}
	return a.updateStateAndReturnResult(ctx, pr, BorrowerStateCheckedIn, nil)
}

func (a *PatronRequestActionService) shipReturnBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest) (events.EventStatus, *events.EventResult) {
	lmsAdapter, err := a.lmsCreator.GetAdapter(ctx, pr.RequesterSymbol)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "failed to create LMS adapter", err)
	}
	item := "" // TODO Get item identifier from the request
	err = lmsAdapter.DeleteItem(item)
	if err != nil {
		return events.LogErrorAndReturnResult(ctx, "LMS DeleteItem failed", err)
	}
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
