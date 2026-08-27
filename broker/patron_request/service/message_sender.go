package prservice

import (
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type PatronRequestMessageSender struct {
	iso18626Handler handler.Iso18626HandlerInterface
	eventBus        events.EventBus
}

var configuredBrokerSymbol = utils.GetEnv("BROKER_SYMBOL", "ISIL:BROKER")

func (ms *PatronRequestMessageSender) sendSupplyingAgencyMessage(ctx common.ExtendedContext, parentEventID string, pr pr_db.PatronRequest, messageInfo iso18626.MessageInfo, statusInfo iso18626.StatusInfo, deliveryInfo *iso18626.DeliveryInfo) (events.EventStatus, *events.EventResult, error) {
	reqAuthority, reqSymbol, err := common.SplitSymbol(pr.RequesterSymbol.String)
	if err != nil {
		return events.EventStatusError, nil, errors.New("invalid requester symbol")
	}
	supAuthority, supSymbol, err := common.SplitSymbol(pr.SupplierSymbol.String)
	if err != nil {
		return events.EventStatusError, nil, errors.New("invalid supplier symbol")
	}
	var illMessage = iso18626.NewISO18626Message()
	illMessage.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: reqAuthority,
				},
				AgencyIdValue: reqSymbol,
			},
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: supAuthority,
				},
				AgencyIdValue: supSymbol,
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
	deliveryStatus := illMessage.SupplyingAgencyMessage.StatusInfo.Status
	if deliveryStatus == iso18626.TypeStatusLoaned {
		if illMessage.SupplyingAgencyMessage.DeliveryInfo == nil {
			illMessage.SupplyingAgencyMessage.DeliveryInfo = &iso18626.DeliveryInfo{}
		}
	}
	if (deliveryStatus == iso18626.TypeStatusLoaned || deliveryStatus == iso18626.TypeStatusCopyCompleted) &&
		illMessage.SupplyingAgencyMessage.DeliveryInfo != nil && illMessage.SupplyingAgencyMessage.DeliveryInfo.DateSent.IsZero() {
		illMessage.SupplyingAgencyMessage.DeliveryInfo.DateSent = utils.XSDDateTime{Time: time.Now()}
	}
	w := NewResponseCaptureWriter()
	ms.iso18626Handler.HandleSupplyingAgencyMessage(ctx, illMessage, w)
	result := &events.EventResult{CommonEventData: events.CommonEventData{
		OutgoingMessage: illMessage,
		IncomingMessage: w.IllMessage,
		HttpFailure:     illHttpFailure(w),
	}}
	status := classifyIllSend(result, w.ParseError)
	ms.createIllMessageNotice(ctx, pr.ID, parentEventID, events.EventNameIllSupplierMessage, result, status)
	return status, result, nil
}

func (ms *PatronRequestMessageSender) sendRequestingAgencyMessage(ctx common.ExtendedContext, parentEventID string, pr pr_db.PatronRequest, action iso18626.TypeAction, note string) (events.EventStatus, *events.EventResult, error) {
	if !pr.SupplierSymbol.Valid {
		return events.EventStatusError, nil, errors.New("missing supplier symbol")
	}
	return ms.sendRequestingAgencyMessageTo(ctx, parentEventID, pr, action, note, pr.SupplierSymbol.String)
}

func (ms *PatronRequestMessageSender) sendRequestingAgencyMessageTo(ctx common.ExtendedContext, parentEventID string, pr pr_db.PatronRequest, action iso18626.TypeAction, note string, supplierSymbol string) (events.EventStatus, *events.EventResult, error) {
	if !pr.RequesterSymbol.Valid {
		return events.EventStatusError, nil, errors.New("missing requester symbol")
	}
	reqAuthority, reqSymbol, err := common.SplitSymbol(pr.RequesterSymbol.String)
	if err != nil {
		return events.EventStatusError, nil, errors.New("invalid requester symbol")
	}
	supAuthority, supSymbol, err := common.SplitSymbol(supplierSymbol)
	if err != nil {
		return events.EventStatusError, nil, errors.New("invalid supplier symbol")
	}
	var illMessage = iso18626.NewISO18626Message()
	illMessage.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{
		Header: iso18626.Header{
			RequestingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: reqAuthority,
				},
				AgencyIdValue: reqSymbol,
			},
			SupplyingAgencyId: iso18626.TypeAgencyId{
				AgencyIdType: iso18626.TypeSchemeValuePair{
					Text: supAuthority,
				},
				AgencyIdValue: supSymbol,
			},
			Timestamp:                 utils.XSDDateTime{Time: time.Now()},
			RequestingAgencyRequestId: pr.ID,
		},
		Action: action,
		Note:   note,
	}
	w := NewResponseCaptureWriter()
	ms.iso18626Handler.HandleRequestingAgencyMessage(ctx, illMessage, w)
	result := &events.EventResult{CommonEventData: events.CommonEventData{
		OutgoingMessage: illMessage,
		IncomingMessage: w.IllMessage,
		HttpFailure:     illHttpFailure(w),
	}}
	status := classifyIllSend(result, w.ParseError)
	ms.createIllMessageNotice(ctx, pr.ID, parentEventID, events.EventNameIllRequesterMessage, result, status)
	return status, result, nil
}

func (ms *PatronRequestMessageSender) sendBorrowingRequest(ctx common.ExtendedContext, parentEventID string, pr pr_db.PatronRequest, request iso18626.Request) (events.EventStatus, *events.EventResult, error) {
	reqAuthority, reqSymbol, err := common.SplitSymbol(pr.RequesterSymbol.String)
	if err != nil {
		return events.EventStatusError, nil, errors.New("invalid requester symbol")
	}

	illRequest, err := deepCopyISO18626Request(request)
	if err != nil {
		return events.EventStatusError, nil, fmt.Errorf("failed to clone outgoing ISO18626 request: %w", err)
	}
	illRequest.Header.RequestingAgencyId = iso18626.TypeAgencyId{
		AgencyIdType: iso18626.TypeSchemeValuePair{
			Text: reqAuthority,
		},
		AgencyIdValue: reqSymbol,
	}
	illRequest.Header.RequestingAgencyRequestId = pr.ID
	illRequest.Header.Timestamp = utils.XSDDateTime{Time: time.Now()}
	if illRequest.PatronInfo == nil {
		illRequest.PatronInfo = &iso18626.PatronInfo{}
	}
	illRequest.PatronInfo.PatronId = pr.Patron.String
	if illRequest.ServiceInfo == nil {
		illRequest.ServiceInfo = &iso18626.ServiceInfo{}
	}
	requestType := iso18626.TypeRequestTypeNew
	illRequest.ServiceInfo.RequestingAgencyPreviousRequestId = ""
	if pr.PrevReqID.Valid {
		illRequest.ServiceInfo.RequestingAgencyPreviousRequestId = pr.PrevReqID.String
		requestType = iso18626.TypeRequestTypeRetry
	}
	illRequest.ServiceInfo.RequestType = &requestType

	var illMessage = iso18626.NewISO18626Message()
	illMessage.Request = &illRequest
	w := NewResponseCaptureWriter()
	resultMap := ms.iso18626Handler.HandleRequest(ctx, illMessage, w)
	var customData map[string]any
	if len(resultMap) > 0 {
		customData = make(map[string]any, len(resultMap))
		for key, value := range resultMap {
			customData[key] = value
		}
	}
	result := &events.EventResult{
		CommonEventData: events.CommonEventData{
			OutgoingMessage: illMessage,
			IncomingMessage: w.IllMessage,
			HttpFailure:     illHttpFailure(w),
		},
		CustomData: customData,
	}
	status := classifyIllSend(result, w.ParseError)
	ms.createIllMessageNotice(ctx, pr.ID, parentEventID, events.EventNameIllRequesterMessage, result, status)
	return status, result, nil
}

type ResponseCaptureWriter struct {
	IllMessage *iso18626.ISO18626Message
	StatusCode int
	Body       []byte
	ParseError error
}

func NewResponseCaptureWriter() *ResponseCaptureWriter {
	return &ResponseCaptureWriter{
		StatusCode: http.StatusOK,
	}
}

func (rcw *ResponseCaptureWriter) Write(b []byte) (int, error) {
	rcw.Body = append(rcw.Body, b...)
	rcw.IllMessage = iso18626.NewISO18626Message()
	rcw.ParseError = xml.Unmarshal(rcw.Body, rcw.IllMessage)
	return len(b), rcw.ParseError
}
func (rcw *ResponseCaptureWriter) WriteHeader(code int) {
	rcw.StatusCode = code
}
func (rcw *ResponseCaptureWriter) Header() http.Header {
	return http.Header{}
}

func illHttpFailure(response *ResponseCaptureWriter) *httpclient.HttpError {
	if response.StatusCode != http.StatusOK {
		return &httpclient.HttpError{
			StatusCode: response.StatusCode,
			Body:       response.Body,
		}
	}
	return nil
}

func classifyIllSend(result *events.EventResult, parseErr error) events.EventStatus {
	if result == nil {
		return events.EventStatusError
	}
	if result.OutgoingMessage == nil {
		result.EventError = &events.EventError{Message: "invalid ILL send result", Cause: "missing outgoing ILL message"}
		return events.EventStatusError
	}
	if result.HttpFailure != nil {
		return events.EventStatusError
	}
	if parseErr != nil {
		result.EventError = &events.EventError{Message: "failed to parse ILL response", Cause: parseErr.Error()}
		return events.EventStatusError
	}
	if result.IncomingMessage == nil {
		result.EventError = &events.EventError{Message: "invalid ILL response", Cause: "missing response"}
		return events.EventStatusError
	}

	var confirmationStatus iso18626.TypeMessageStatus
	switch {
	case result.OutgoingMessage.Request != nil:
		if result.IncomingMessage.RequestConfirmation == nil {
			return invalidIllConfirmation(result, "requestConfirmation")
		}
		confirmationStatus = result.IncomingMessage.RequestConfirmation.ConfirmationHeader.MessageStatus
	case result.OutgoingMessage.RequestingAgencyMessage != nil:
		if result.IncomingMessage.RequestingAgencyMessageConfirmation == nil {
			return invalidIllConfirmation(result, "requestingAgencyMessageConfirmation")
		}
		confirmationStatus = result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus
	case result.OutgoingMessage.SupplyingAgencyMessage != nil:
		if result.IncomingMessage.SupplyingAgencyMessageConfirmation == nil {
			return invalidIllConfirmation(result, "supplyingAgencyMessageConfirmation")
		}
		confirmationStatus = result.IncomingMessage.SupplyingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus
	default:
		result.EventError = &events.EventError{Message: "invalid ILL send result", Cause: "unsupported outgoing ILL message"}
		return events.EventStatusError
	}

	switch confirmationStatus {
	case iso18626.TypeMessageStatusOK:
		return events.EventStatusSuccess
	case iso18626.TypeMessageStatusERROR:
		return events.EventStatusProblem
	default:
		result.EventError = &events.EventError{
			Message: "invalid ILL response",
			Cause:   fmt.Sprintf("invalid confirmation message status %q", confirmationStatus),
		}
		return events.EventStatusError
	}
}

func invalidIllConfirmation(result *events.EventResult, expected string) events.EventStatus {
	actual := illMessageType(result.IncomingMessage)
	result.EventError = &events.EventError{
		Message: "invalid ILL response",
		Cause:   fmt.Sprintf("expected %s, received %s", expected, actual),
	}
	return events.EventStatusError
}

func illMessageType(message *iso18626.ISO18626Message) string {
	switch {
	case message == nil:
		return "no message"
	case message.Request != nil:
		return "request"
	case message.RequestConfirmation != nil:
		return "requestConfirmation"
	case message.RequestingAgencyMessage != nil:
		return "requestingAgencyMessage"
	case message.RequestingAgencyMessageConfirmation != nil:
		return "requestingAgencyMessageConfirmation"
	case message.SupplyingAgencyMessage != nil:
		return "supplyingAgencyMessage"
	case message.SupplyingAgencyMessageConfirmation != nil:
		return "supplyingAgencyMessageConfirmation"
	default:
		return "empty message"
	}
}

func (ms *PatronRequestMessageSender) createIllMessageNotice(ctx common.ExtendedContext, prID, parentEventID string, eventName events.EventName, result *events.EventResult, status events.EventStatus) {
	if ms.eventBus == nil {
		return
	}
	eventData := events.EventData{
		CommonEventData: result.CommonEventData,
		CustomData:      result.CustomData,
	}
	var err error
	if parentEventID == "" {
		_, err = ms.eventBus.CreateNotice(prID, eventName, eventData, status, events.EventDomainPatronRequest, events.SignalConsumers)
	} else {
		_, err = ms.eventBus.CreateNoticeWithParent(prID, eventName, eventData, status, events.EventDomainPatronRequest, &parentEventID, events.SignalConsumers)
	}
	if err != nil {
		ctx.Logger().Error("failed to create ILL message log event", "error", err)
	}
}
