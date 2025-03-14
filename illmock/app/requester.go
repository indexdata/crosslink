package app

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/illmock/role"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type requesterInfo struct {
	cancel      bool
	renew       bool
	received    bool
	supplierUrl string
	request     *iso18626.Request
}

type Requester struct {
	requestingAgencyId string
	supplyingAgencyId  string
	requests           sync.Map
}

func (r *Requester) getKey(header *iso18626.Header) string {
	return header.SupplyingAgencyId.AgencyIdValue + "/" + header.RequestingAgencyRequestId
}

func (r *Requester) load(header *iso18626.Header) *requesterInfo {
	v, ok := r.requests.Load(r.getKey(header))
	if !ok {
		return nil
	}
	return v.(*requesterInfo)
}

func (r *Requester) store(header *iso18626.Header, info *requesterInfo) {
	r.requests.Store(r.getKey(header), info)
}

func (s *Requester) delete(header *iso18626.Header) {
	s.requests.Delete(s.getKey(header))
}

func createRequest() *iso18626.Iso18626MessageNS {
	var msg = iso18626.NewIso18626MessageNS()
	msg.Request = &iso18626.Request{}
	return msg
}

func (app *MockApp) handlePatronRequest(illMessage *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	illRequest := illMessage.Request

	requester := &app.requester
	msg := createRequest()
	msg.Request.Header = illRequest.Header
	msg.Request.BibliographicInfo = illRequest.BibliographicInfo
	msg.Request.PublicationInfo = illRequest.PublicationInfo
	if illRequest.ServiceInfo != nil {
		msg.Request.ServiceInfo = &iso18626.ServiceInfo{}
		*msg.Request.ServiceInfo = *illRequest.ServiceInfo
		msg.Request.ServiceInfo.RequestSubType = nil // not a patron request any more
	}
	msg.Request.SupplierInfo = illRequest.SupplierInfo
	msg.Request.RequestedDeliveryInfo = illRequest.RequestedDeliveryInfo
	msg.Request.RequestingAgencyInfo = illRequest.RequestingAgencyInfo
	msg.Request.PatronInfo = illRequest.PatronInfo
	msg.Request.BillingInfo = illRequest.BillingInfo
	header := &msg.Request.Header

	// ServiceInfo != nil already
	cancel := illRequest.ServiceInfo.Note == "#CANCEL#"
	renew := illRequest.ServiceInfo.Note == "#RENEW#"

	// patron may omit RequestingAgencyRequestId
	if header.RequestingAgencyRequestId == "" {
		header.RequestingAgencyRequestId = uuid.NewString()
	}
	if header.RequestingAgencyId.AgencyIdType.Text == "" {
		header.RequestingAgencyId.AgencyIdType.Text = app.agencyType
	}
	if header.RequestingAgencyId.AgencyIdValue == "" {
		header.RequestingAgencyId.AgencyIdValue = requester.requestingAgencyId
	}
	if header.SupplyingAgencyId.AgencyIdType.Text == "" {
		header.SupplyingAgencyId.AgencyIdType.Text = app.agencyType
	}
	if header.SupplyingAgencyId.AgencyIdValue == "" {
		header.SupplyingAgencyId.AgencyIdValue = requester.supplyingAgencyId
	}
	header.Timestamp = utils.XSDDateTime{Time: time.Now()}

	app.logIncomingReq(role.Requester, header, illMessage)

	requesterInfo := &requesterInfo{supplierUrl: app.peerUrl, cancel: cancel, renew: renew, request: msg.Request}
	for _, supplierInfo := range illRequest.SupplierInfo {
		description := supplierInfo.SupplierDescription
		if strings.HasPrefix(description, "http://") || strings.HasPrefix(description, "https://") {
			requesterInfo.supplierUrl = description
			break
		}
	}
	responseMsg, err := app.sendReceive(requesterInfo.supplierUrl, msg, role.Requester, header)
	if err != nil {
		errorMessage := fmt.Sprintf("Error sending request to supplier: %s", err.Error())
		app.handleRequestError(header, role.Requester, errorMessage, iso18626.TypeErrorTypeUnrecognisedDataElement, w)
		return
	}
	requestConfirmation := responseMsg.RequestConfirmation
	if requestConfirmation == nil {
		app.handleRequestError(header, role.Requester, "Did not receive requestConfirmation from supplier", iso18626.TypeErrorTypeUnrecognisedDataElement, w)
		return
	}
	requester.store(header, requesterInfo)

	var resmsg = createRequestResponse(header, iso18626.TypeMessageStatusOK, nil, nil)
	resmsg.RequestConfirmation.ErrorData = requestConfirmation.ErrorData
	resmsg.RequestConfirmation.ConfirmationHeader.MessageStatus = requestConfirmation.ConfirmationHeader.MessageStatus
	app.writeIso18626Response(resmsg, w, role.Requester, header)
}

func (app *MockApp) sendRequestingAgencyMessageDelay(header *iso18626.Header, action iso18626.TypeAction) {
	time.Sleep(app.messageDelay)
	app.sendRequestingAgencyMessage(header, action)
}

func (app *MockApp) sendRequestingAgencyMessage(header *iso18626.Header, action iso18626.TypeAction) {
	requester := &app.requester
	state := requester.load(header)
	if state == nil {
		log.Warn("sendRequestingAgencyMessage request gone", "key", requester.getKey(header))
		return
	}
	msg := createRequestingAgencyMessage()
	msg.RequestingAgencyMessage.Header = *header
	msg.RequestingAgencyMessage.Action = action

	responseMsg, err := app.sendReceive(state.supplierUrl, msg, "requester", header)
	if err != nil {
		log.Warn("sendRequestingAgencyMessage", "url", state.supplierUrl, "error", err.Error())
		return
	}
	if responseMsg.RequestingAgencyMessageConfirmation == nil {
		log.Warn("sendRequestingAgencyMessage did not receive RequestingAgencyMessageConfirmation")
		return
	}
	if responseMsg.RequestingAgencyMessageConfirmation.Action == nil {
		log.Warn("sendRequestingAgencyMessage did not receive action in confirmation", "action", action)
		return
	}

	if *responseMsg.RequestingAgencyMessageConfirmation.Action != action {
		log.Warn("sendRequestingAgencyMessage did not receive same action in confirmation", "action", action,
			"got", responseMsg.RequestingAgencyMessageConfirmation.Action)
		return
	}
	if action == iso18626.TypeActionReceived {
		go app.sendRequestingAgencyMessageDelay(header, iso18626.TypeActionShippedReturn)
	}
}

func createSupplyingAgencyResponse(supplyingAgencyMessage *iso18626.SupplyingAgencyMessage, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = iso18626.NewIso18626MessageNS()
	header := createConfirmationHeader(&supplyingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func (app *MockApp) handleSupplyingAgencyError(illMessage *iso18626.SupplyingAgencyMessage, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	app.writeIso18626Response(resmsg, w, role.Requester, &illMessage.Header)
}

func createRequestingAgencyMessage() *iso18626.Iso18626MessageNS {
	var msg = iso18626.NewIso18626MessageNS()
	msg.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{}
	return msg
}

func (app *MockApp) sendRetryRequest(illRequest *iso18626.Request, supplierUrl string, messageInfo *iso18626.MessageInfo) {
	msg := &iso18626.Iso18626MessageNS{}
	msg.Request = illRequest
	msg.Request.ServiceInfo = &iso18626.ServiceInfo{}
	if msg.Request.ServiceInfo != nil {
		*msg.Request.ServiceInfo = *illRequest.ServiceInfo
	}
	msg.Request.ServiceInfo.RequestingAgencyPreviousRequestId = illRequest.Header.RequestingAgencyRequestId
	requestType := iso18626.TypeRequestTypeRetry
	msg.Request.ServiceInfo.RequestType = &requestType
	if messageInfo.ReasonRetry != nil && messageInfo.ReasonRetry.Text == string(iso18626.ReasonRetryCostExceedsMaxCost) {
		offered := *messageInfo.OfferedCosts
		log.Info("offeredCosts", "offered", offered)
		msg.Request.BillingInfo = &iso18626.BillingInfo{}
		msg.Request.BillingInfo.MaximumCosts = &offered
	}
	_, err := app.sendReceive(supplierUrl, msg, role.Requester, &illRequest.Header)
	if err != nil {
		log.Warn("sendRetryRequest", "url", supplierUrl, "error", err.Error())
	}
}

func (app *MockApp) handleIso18626SupplyingAgencyMessage(illMessage *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	requester := &app.requester
	supplyingAgencyMessage := illMessage.SupplyingAgencyMessage
	header := &supplyingAgencyMessage.Header
	app.logIncomingReq(role.Requester, header, illMessage)
	err := validateHeader(header)
	if err != nil {
		app.handleSupplyingAgencyError(supplyingAgencyMessage, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	state := requester.load(header)
	if state == nil {
		app.handleSupplyingAgencyError(supplyingAgencyMessage, "Non existing RequestingAgencyRequestId", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	resmsg := createSupplyingAgencyResponse(supplyingAgencyMessage, iso18626.TypeMessageStatusOK, nil, nil)
	reason := supplyingAgencyMessage.MessageInfo.ReasonForMessage
	resmsg.SupplyingAgencyMessageConfirmation.ReasonForMessage = &reason
	app.writeIso18626Response(resmsg, w, role.Requester, header)
	if state.cancel {
		state.cancel = false
		go app.sendRequestingAgencyMessage(header, iso18626.TypeActionCancel)
		return
	}
	switch supplyingAgencyMessage.StatusInfo.Status {
	case iso18626.TypeStatusLoaned:
		if !state.received {
			state.received = true
			go app.sendRequestingAgencyMessage(header, iso18626.TypeActionReceived)
		}
	case iso18626.TypeStatusOverdue:
		if state.renew {
			state.renew = false
			go app.sendRequestingAgencyMessage(header, iso18626.TypeActionRenew)
		}
	case iso18626.TypeStatusLoanCompleted:
		requester.delete(header)
	case iso18626.TypeStatusUnfilled:
		requester.delete(header)
	case iso18626.TypeStatusCancelled:
		if supplyingAgencyMessage.MessageInfo.AnswerYesNo != nil {
			if *supplyingAgencyMessage.MessageInfo.AnswerYesNo == iso18626.TypeYesNoY {
				requester.delete(header)
			}
		}
	case iso18626.TypeStatusRetryPossible:
		go app.sendRetryRequest(state.request, state.supplierUrl, &supplyingAgencyMessage.MessageInfo)
	}
}
