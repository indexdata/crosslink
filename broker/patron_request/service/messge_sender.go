package prservice

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type PatronRequestMessageSender struct {
	iso18626Handler handler.Iso18626HandlerInterface
}

func (ms *PatronRequestMessageSender) sendSupplyingAgencyMessage(ctx common.ExtendedContext, pr pr_db.PatronRequest, result *events.EventResult, messageInfo iso18626.MessageInfo, statusInfo iso18626.StatusInfo, deliveryInfo *iso18626.DeliveryInfo) (events.EventStatus, *events.EventResult, *int) {
	if !pr.RequesterSymbol.Valid {
		status, eventResult := logErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return status, eventResult, nil
	}
	// pr.SupplierSymbol is validated earlier in handleLenderAction
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	supplierSymbol := strings.SplitN(pr.SupplierSymbol.String, ":", 2)
	var illMessage = iso18626.NewISO18626Message()
	illMessage.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
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
			Timestamp:                 utils.XSDDateTime{Time: time.Now()},
			RequestingAgencyRequestId: pr.RequesterReqID.String,
			SupplyingAgencyRequestId:  pr.ID,
		},
		MessageInfo:  messageInfo,
		StatusInfo:   statusInfo,
		DeliveryInfo: deliveryInfo,
	}
	if illMessage.SupplyingAgencyMessage.StatusInfo.LastChange.IsZero() {
		illMessage.SupplyingAgencyMessage.StatusInfo.LastChange = utils.XSDDateTime{Time: time.Now()}
	}
	if illMessage.SupplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoaned {
		if illMessage.SupplyingAgencyMessage.DeliveryInfo == nil {
			illMessage.SupplyingAgencyMessage.DeliveryInfo = &iso18626.DeliveryInfo{}
		}
		if illMessage.SupplyingAgencyMessage.DeliveryInfo.DateSent.IsZero() {
			illMessage.SupplyingAgencyMessage.DeliveryInfo.DateSent = utils.XSDDateTime{Time: time.Now()}
		}
	}
	w := NewResponseCaptureWriter()
	ms.iso18626Handler.HandleSupplyingAgencyMessage(ctx, illMessage, w)
	result.OutgoingMessage = illMessage
	result.IncomingMessage = w.IllMessage
	return "", nil, &w.StatusCode
}

func (ms *PatronRequestMessageSender) sendRequestingAgencyMessage(ctx common.ExtendedContext, pr pr_db.PatronRequest, result *events.EventResult, action iso18626.TypeAction, note string) (events.EventStatus, *events.EventResult, *int) {
	if !pr.RequesterSymbol.Valid {
		status, eventResult := logErrorAndReturnResult(ctx, "missing requester symbol", nil)
		return status, eventResult, nil
	}
	if !pr.SupplierSymbol.Valid {
		status, eventResult := logErrorAndReturnResult(ctx, "missing supplier symbol", nil)
		return status, eventResult, nil
	}
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	if len(requesterSymbol) != 2 {
		status, eventResult := logErrorAndReturnResult(ctx, "invalid requester symbol", nil)
		return status, eventResult, nil
	}
	supplierSymbol := strings.SplitN(pr.SupplierSymbol.String, ":", 2)
	if len(supplierSymbol) != 2 {
		status, eventResult := logErrorAndReturnResult(ctx, "invalid supplier symbol", nil)
		return status, eventResult, nil
	}
	var illMessage = iso18626.NewISO18626Message()
	illMessage.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
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
			Timestamp:                 utils.XSDDateTime{Time: time.Now()},
			RequestingAgencyRequestId: pr.ID,
		},
		Action: action,
		Note:   note,
	}
	w := NewResponseCaptureWriter()
	ms.iso18626Handler.HandleRequestingAgencyMessage(ctx, illMessage, w)
	result.OutgoingMessage = illMessage
	result.IncomingMessage = w.IllMessage
	return "", nil, &w.StatusCode
}

func (ms *PatronRequestMessageSender) sendBorrowingRequest(ctx common.ExtendedContext, pr pr_db.PatronRequest, request iso18626.Request) actionExecutionResult {
	result := events.EventResult{}
	// pr.RequesterSymbol is validated earlier in handleBorrowingAction
	requesterSymbol := strings.SplitN(pr.RequesterSymbol.String, ":", 2)
	if len(requesterSymbol) != 2 {
		status, eventResult := logErrorAndReturnResult(ctx, "invalid requester symbol", nil)
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}

	illRequest, err := deepCopyISO18626Request(request)
	if err != nil {
		status, eventResult := logErrorAndReturnResult(ctx, "failed to clone outgoing ISO18626 request", err)
		return actionExecutionResult{status: status, result: eventResult, pr: pr}
	}
	illRequest.Header.RequestingAgencyId = iso18626.TypeAgencyId{
		AgencyIdType: iso18626.TypeSchemeValuePair{
			Text: requesterSymbol[0],
		},
		AgencyIdValue: requesterSymbol[1],
	}
	illRequest.Header.RequestingAgencyRequestId = pr.ID
	illRequest.Header.Timestamp = utils.XSDDateTime{Time: time.Now()}
	if illRequest.PatronInfo == nil {
		illRequest.PatronInfo = &iso18626.PatronInfo{}
	}
	illRequest.PatronInfo.PatronId = pr.Patron.String

	var illMessage = iso18626.NewISO18626Message()
	illMessage.Request = &illRequest
	w := NewResponseCaptureWriter()
	ms.iso18626Handler.HandleRequest(ctx, illMessage, w)
	result.OutgoingMessage = illMessage
	result.IncomingMessage = w.IllMessage
	if w.StatusCode != http.StatusOK || w.IllMessage == nil || w.IllMessage.RequestConfirmation == nil ||
		w.IllMessage.RequestConfirmation.ConfirmationHeader.MessageStatus != iso18626.TypeMessageStatusOK {
		result.ActionResult = &events.ActionResult{Outcome: ActionOutcomeFailure}
		return actionExecutionResult{status: events.EventStatusProblem, result: &result, pr: pr}
	}
	return actionExecutionResult{status: events.EventStatusSuccess, result: &result, pr: pr}
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
	rcw.IllMessage = iso18626.NewISO18626Message()
	err := xml.Unmarshal(b, rcw.IllMessage)
	return 1, err
}
func (rcw *ResponseCaptureWriter) WriteHeader(code int) {
	rcw.StatusCode = code
}
func (rcw *ResponseCaptureWriter) Header() http.Header {
	return http.Header{}
}
