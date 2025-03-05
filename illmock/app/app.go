package app

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/httpclient"
	"github.com/indexdata/crosslink/illmock/flows"
	"github.com/indexdata/crosslink/illmock/netutil"
	"github.com/indexdata/crosslink/illmock/role"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/crosslink/illmock/sruapi"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type requesterInfo struct {
	action      iso18626.TypeAction
	supplierUrl string
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

type supplierInfo struct {
	index             int                   // index into status below
	status            []iso18626.TypeStatus // the status that the supplier will return
	supplierRequestId string                // supplier request Id
	requesterUrl      string                // requester URL
}

type Supplier struct {
	requests sync.Map
}

func (s *Supplier) getKey(header *iso18626.Header) string {
	return header.SupplyingAgencyId.AgencyIdValue + "/" + header.RequestingAgencyId.AgencyIdValue + "/" + header.RequestingAgencyRequestId
}

func (s *Supplier) load(header *iso18626.Header) *supplierInfo {
	v, ok := s.requests.Load(s.getKey(header))
	if !ok {
		return nil
	}
	return v.(*supplierInfo)
}

func (s *Supplier) store(header *iso18626.Header, info *supplierInfo) {
	s.requests.Store(s.getKey(header), info)
}

func (s *Supplier) delete(header *iso18626.Header) {
	s.requests.Delete(s.getKey(header))
}

type MockApp struct {
	httpPort       string
	agencyType     string
	peerUrl        string
	supplyDuration time.Duration
	server         *http.Server
	requester      Requester
	supplier       Supplier
	flowsApi       *flows.FlowsApi
	sruApi         *sruapi.SruApi
}

var log *slog.Logger = slogwrap.SlogWrap()

func validateHeader(header *iso18626.Header) error {
	if header.RequestingAgencyRequestId == "" {
		return fmt.Errorf("RequestingAgencyRequestId cannot be empty")
	}
	if header.RequestingAgencyId.AgencyIdValue == "" {
		return fmt.Errorf("RequestingAgencyId cannot be empty")
	}
	if header.SupplyingAgencyId.AgencyIdValue == "" {
		return fmt.Errorf("SupplyingAgencyId cannot be empty")
	}
	return nil
}

func (app *MockApp) writeIso18626Response(resmsg *iso18626.Iso18626MessageNS, w http.ResponseWriter, role role.Role, header *iso18626.Header) {
	buf := utils.Must(xml.MarshalIndent(resmsg, "  ", "  "))
	if buf == nil {
		http.Error(w, "marshal failed", http.StatusInternalServerError)
		return
	}
	app.logOutgoingRes(role, header, resmsg)
	w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
	netutil.WriteHttpResponse(w, buf)
}

func createConfirmationHeader(inHeader *iso18626.Header, messageStatus iso18626.TypeMessageStatus) *iso18626.ConfirmationHeader {
	var header = &iso18626.ConfirmationHeader{}
	header.RequestingAgencyId = &iso18626.TypeAgencyId{}
	header.RequestingAgencyId.AgencyIdType = inHeader.RequestingAgencyId.AgencyIdType
	header.RequestingAgencyId.AgencyIdValue = inHeader.RequestingAgencyId.AgencyIdValue
	header.TimestampReceived = inHeader.Timestamp
	header.RequestingAgencyRequestId = inHeader.RequestingAgencyRequestId

	if len(inHeader.SupplyingAgencyId.AgencyIdValue) != 0 {
		header.SupplyingAgencyId = &iso18626.TypeAgencyId{}
		header.SupplyingAgencyId.AgencyIdType = inHeader.SupplyingAgencyId.AgencyIdType
		header.SupplyingAgencyId.AgencyIdValue = inHeader.SupplyingAgencyId.AgencyIdValue
	}

	header.Timestamp = utils.XSDDateTime{Time: time.Now()}
	header.MessageStatus = messageStatus
	return header
}

func createErrorData(errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.ErrorData {
	if errorMessage != nil {
		var errorData = iso18626.ErrorData{
			ErrorType:  *errorType,
			ErrorValue: *errorMessage,
		}
		return &errorData
	}
	return nil
}

func createRequestResponse(requestHeader *iso18626.Header, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = iso18626.NewIso18626MessageNS()
	header := createConfirmationHeader(requestHeader, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func createRequest() *iso18626.Iso18626MessageNS {
	var msg = iso18626.NewIso18626MessageNS()
	msg.Request = &iso18626.Request{}
	return msg
}

func (app *MockApp) handleRequestError(requestHeader *iso18626.Header, role role.Role, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestResponse(requestHeader, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	app.writeIso18626Response(resmsg, w, role, requestHeader)
}

func (app *MockApp) handleRequestingAgencyMessageError(request *iso18626.RequestingAgencyMessage, role role.Role, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestingAgencyConfirmation(&request.Header, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	resmsg.RequestingAgencyMessageConfirmation.Action = &request.Action
	app.writeIso18626Response(resmsg, w, role, &request.Header)
}

func (app *MockApp) sendReceive(url string, msg *iso18626.Iso18626MessageNS, role role.Role, header *iso18626.Header) (*iso18626.Iso18626MessageNS, error) {
	if url == "" {
		return nil, fmt.Errorf("url cannot be empty")
	}
	url = url + "/iso18626"
	app.logOutgoingReq(role, header, msg, url)
	var response iso18626.Iso18626MessageNS
	err := httpclient.PostXml(http.DefaultClient, url, msg, &response)
	if err != nil {
		status := 0
		if httpErr, ok := err.(*httpclient.HttpError); ok {
			status = httpErr.StatusCode
		}
		app.logOutgoingErr(role, header, url, status, err.Error())
		return nil, err
	}
	app.logIncomingRes(role, header, &response, url)
	return &response, nil
}

func (app *MockApp) handlePatronRequest(illMessage *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	illRequest := illMessage.Request

	patronReqHeader := illRequest.Header
	requester := &app.requester
	msg := createRequest()
	msg.Request.Header = illRequest.Header
	msg.Request.BibliographicInfo = illRequest.BibliographicInfo
	msg.Request.PublicationInfo = illRequest.PublicationInfo
	msg.Request.ServiceInfo = nil // not a patron request any more
	msg.Request.SupplierInfo = illRequest.SupplierInfo
	msg.Request.RequestedDeliveryInfo = illRequest.RequestedDeliveryInfo
	msg.Request.RequestingAgencyInfo = illRequest.RequestingAgencyInfo
	msg.Request.PatronInfo = illRequest.PatronInfo
	msg.Request.BillingInfo = illRequest.BillingInfo
	header := &msg.Request.Header

	// ServiceInfo != nil already
	action := iso18626.TypeActionReceived
	if illRequest.ServiceInfo.Note == "#CANCEL#" {
		action = iso18626.TypeActionCancel
	}

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

	requesterInfo := &requesterInfo{action: action, supplierUrl: app.peerUrl}
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
		app.handleRequestError(&patronReqHeader, role.Requester, errorMessage, iso18626.TypeErrorTypeUnrecognisedDataElement, w)
		return
	}
	requestConfirmation := responseMsg.RequestConfirmation
	if requestConfirmation == nil {
		app.handleRequestError(&patronReqHeader, role.Requester, "Did not receive requestConfirmation from supplier", iso18626.TypeErrorTypeUnrecognisedDataElement, w)
		return
	}
	requester.store(header, requesterInfo)

	var resmsg = createRequestResponse(&patronReqHeader, iso18626.TypeMessageStatusOK, nil, nil)
	resmsg.RequestConfirmation.ErrorData = requestConfirmation.ErrorData
	resmsg.RequestConfirmation.ConfirmationHeader.MessageStatus = requestConfirmation.ConfirmationHeader.MessageStatus
	app.writeIso18626Response(resmsg, w, role.Requester, header)
}

func (app *MockApp) handleSupplierRequest(illRequest *iso18626.Request, w http.ResponseWriter) {
	supplier := &app.supplier
	err := validateHeader(&illRequest.Header)
	if err != nil {
		app.handleRequestError(&illRequest.Header, role.Supplier, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	state := supplier.load(&illRequest.Header)
	if state != nil {
		app.handleRequestError(&illRequest.Header, role.Supplier, "RequestingAgencyRequestId already exists", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	var status []iso18626.TypeStatus
	// should be able to parse the value and put any types into status...
	switch illRequest.BibliographicInfo.SupplierUniqueRecordId {
	case "WILLSUPPLY_LOANED":
		status = append(status, iso18626.TypeStatusWillSupply, iso18626.TypeStatusLoaned)
	case "WILLSUPPLY_UNFILLED":
		status = append(status, iso18626.TypeStatusWillSupply, iso18626.TypeStatusUnfilled)
	case "UNFILLED":
		status = append(status, iso18626.TypeStatusUnfilled)
	case "LOANED":
		status = append(status, iso18626.TypeStatusLoaned)
	case "ERROR":
		log.Warn("handleSupplierRequest ERROR")
		app.handleRequestError(&illRequest.Header, role.Supplier, "MOCK ERROR", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	case "HTTP-ERROR-400":
		http.Error(w, "MOCK HTTP-ERROR-400", http.StatusBadRequest)
		return
	case "HTTP-ERROR-500":
		http.Error(w, "MOCK HTTP-ERROR-500", http.StatusInternalServerError)
		return
	default:
		status = append(status, iso18626.TypeStatusUnfilled)
	}

	supplierInfo := &supplierInfo{status: status, index: 0, supplierRequestId: uuid.NewString(), requesterUrl: app.peerUrl}
	requestingAgencyInfo := illRequest.RequestingAgencyInfo
	if requestingAgencyInfo != nil {
		for _, address := range requestingAgencyInfo.Address {
			electronicAddress := address.ElectronicAddress
			if electronicAddress != nil {
				data := electronicAddress.ElectronicAddressData
				if strings.HasPrefix(data, "http://") || strings.HasPrefix(data, "https://") {
					supplierInfo.requesterUrl = data
					break
				}
			}
		}
	}
	supplier.store(&illRequest.Header, supplierInfo)

	var resmsg = createRequestResponse(&illRequest.Header, iso18626.TypeMessageStatusOK, nil, nil)
	app.writeIso18626Response(resmsg, w, role.Supplier, &illRequest.Header)
	go app.sendSupplyingAgencyLater(&illRequest.Header)
}

func logMessage(lead string, illMessage *iso18626.Iso18626MessageNS) bool {
	buf := utils.Must(xml.MarshalIndent(illMessage, "  ", "  "))
	if buf == nil {
		return false
	}
	log.Info(fmt.Sprintf("%s\n%s", lead, buf))
	return true
}

func (app *MockApp) logIso18626Message(role role.Role, kind string, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS, extra string) {
	logmsg := fmt.Sprintf("%s role:%s id:%s req:%s sup:%s%s", kind, role, header.RequestingAgencyRequestId,
		header.RequestingAgencyId.AgencyIdValue, header.SupplyingAgencyId.AgencyIdValue, extra)
	if logMessage(logmsg, illMessage) {
		flowMessage := flows.FlowMessage{Kind: kind, Timestamp: header.Timestamp, Message: *illMessage}
		app.flowsApi.AddFlow(flows.Flow{Message: []flows.FlowMessage{flowMessage}, Id: header.RequestingAgencyRequestId, Role: role,
			Supplier: header.SupplyingAgencyId.AgencyIdValue, Requester: header.RequestingAgencyId.AgencyIdValue})
	}
}

func (app *MockApp) logIncomingReq(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS) {
	app.logIso18626Message(role, "incoming-request", header, illMessage, "")
}

func (app *MockApp) logOutgoingReq(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS,
	url string) {
	app.logIso18626Message(role, "outgoing-request", header, illMessage, fmt.Sprintf(" url:%s", url))
}

func (app *MockApp) logOutgoingErr(role role.Role, header *iso18626.Header, url string, status int, error string) {
	log.Info(fmt.Sprintf("outgoing-error role:%s id:%s req:%s sup:%s url:%s status:%d error:%s", role, header.RequestingAgencyRequestId,
		header.RequestingAgencyId.AgencyIdValue, header.SupplyingAgencyId.AgencyIdValue, url, status, error))
}

func (app *MockApp) logIncomingRes(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS,
	url string) {
	app.logIso18626Message(role, "incoming-response", header, illMessage, fmt.Sprintf(" url:%s", url))
}

func (app *MockApp) logOutgoingRes(role role.Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS) {
	app.logIso18626Message(role, "outgoing-response", header, illMessage, "")
}

func createSupplyingAgencyMessage() *iso18626.Iso18626MessageNS {
	var msg = iso18626.NewIso18626MessageNS()
	msg.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
	return msg
}

func (app *MockApp) sendSupplyingAgencyMessage(header *iso18626.Header, state *supplierInfo, msg *iso18626.Iso18626MessageNS) bool {
	responseMsg, err := app.sendReceive(state.requesterUrl, msg, role.Supplier, header)
	if err != nil {
		log.Warn("sendSupplyingAgencyCancel", "error", err.Error())
		return false
	}
	if responseMsg.SupplyingAgencyMessageConfirmation == nil {
		log.Warn("sendSupplyingAgencyCancel did not receive SupplyingAgencyMessageConfirmation")
		return false
	}
	return true
}

func (app *MockApp) sendSupplyingAgencyCancel(header *iso18626.Header, state *supplierInfo) {
	msg := createSupplyingAgencyMessage()
	msg.SupplyingAgencyMessage.Header = *header
	msg.SupplyingAgencyMessage.StatusInfo.Status = iso18626.TypeStatusCancelled
	msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageCancelResponse
	var answer iso18626.TypeYesNo = iso18626.TypeYesNoY
	for i := 0; i < state.index; i++ {
		if state.status[i] == iso18626.TypeStatusLoaned {
			answer = iso18626.TypeYesNoN
			break
		}
	}
	msg.SupplyingAgencyMessage.MessageInfo.AnswerYesNo = &answer
	app.sendSupplyingAgencyMessage(header, state, msg)
}

func (app *MockApp) sendSupplyingAgencyLater(header *iso18626.Header) {
	time.Sleep(app.supplyDuration)

	msg := createSupplyingAgencyMessage()
	msg.SupplyingAgencyMessage.Header = *header

	supplier := &app.supplier
	state := supplier.load(header)
	if state == nil {
		log.Warn("sendSupplyingAgencyMessage no key", "key", supplier.getKey(header))
		return
	}
	msg.SupplyingAgencyMessage.Header.SupplyingAgencyRequestId = state.supplierRequestId
	msg.SupplyingAgencyMessage.StatusInfo.Status = state.status[state.index]
	if state.status[state.index] == iso18626.TypeStatusLoanCompleted {
		supplier.delete(header)
	}
	log.Info("sendSupplyingAgencyMessage", "status", state.status[state.index], "index", state.index)

	if state.index == 0 {
		msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageRequestResponse
	} else {
		msg.SupplyingAgencyMessage.MessageInfo.ReasonForMessage = iso18626.TypeReasonForMessageStatusChange
	}
	state.index++
	if app.sendSupplyingAgencyMessage(header, state, msg) {
		if state.index < len(state.status) {
			go app.sendSupplyingAgencyLater(header)
		}
	}
}

func createRequestingAgencyConfirmation(iheader *iso18626.Header, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = iso18626.NewIso18626MessageNS()
	header := createConfirmationHeader(iheader, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func (app *MockApp) handleIso18626RequestingAgencyMessage(illMessage *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	requestingAgencyMessage := illMessage.RequestingAgencyMessage
	app.logIncomingReq(role.Supplier, &requestingAgencyMessage.Header, illMessage)
	err := validateHeader(&requestingAgencyMessage.Header)
	if err != nil {
		app.handleRequestingAgencyMessageError(requestingAgencyMessage, role.Supplier, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	var resmsg = createRequestingAgencyConfirmation(&requestingAgencyMessage.Header, iso18626.TypeMessageStatusOK, nil, nil)
	resmsg.RequestingAgencyMessageConfirmation.Action = &requestingAgencyMessage.Action
	app.writeIso18626Response(resmsg, w, role.Supplier, &requestingAgencyMessage.Header)

	header := &requestingAgencyMessage.Header
	supplier := &app.supplier
	state := supplier.load(header)
	if state == nil {
		log.Warn("sendSupplyingAgencyMessage no key", "key", supplier.getKey(header))
		return
	}
	if requestingAgencyMessage.Action == iso18626.TypeActionCancel {
		app.sendSupplyingAgencyCancel(header, state)
		return
	}
	if requestingAgencyMessage.Action != iso18626.TypeActionShippedReturn {
		return
	}
	state.index = 0
	state.status = []iso18626.TypeStatus{iso18626.TypeStatusLoanCompleted}
	go app.sendSupplyingAgencyLater(header)
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

	if state.action == iso18626.TypeActionCancel ||
		supplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoaned {
		go app.sendRequestingAgencyMessage(header)
	}
	if supplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoanCompleted {
		log.Info("handleIso18626SupplyingAgencyMessage supplier loan completed delete")
		requester.delete(header)
	}
	if supplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusCancelled &&
		supplyingAgencyMessage.MessageInfo.AnswerYesNo != nil {
		if *supplyingAgencyMessage.MessageInfo.AnswerYesNo == iso18626.TypeYesNoY {
			log.Info("handleIso18626SupplyingAgencyMessage cancelled delete")
			requester.delete(header)
		}
	}
}

func (app *MockApp) sendRequestingAgencyMessage(header *iso18626.Header) {
	requester := &app.requester
	state := requester.load(header)
	if state == nil {
		log.Warn("sendRequestingAgencyMessage request gone", "key", requester.getKey(header))
		return
	}

	action := state.action
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
	if responseMsg.RequestingAgencyMessageConfirmation.Action == nil || *responseMsg.RequestingAgencyMessageConfirmation.Action != action {
		log.Warn("sendRequestingAgencyMessage did not receive same action in confirmation")
		return
	}
	if state.action == iso18626.TypeActionCancel {
		state.action = ""
	}
	if state.action == iso18626.TypeActionReceived {
		state.action = iso18626.TypeActionShippedReturn
		go app.sendRequestingAgencyMessage(header)
	}
}

func healthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
			return
		}
		netutil.WriteHttpResponse(w, []byte("OK\r\n"))
	}
}

func iso18626Handler(app *MockApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get(httpclient.ContentType)
		if !strings.HasPrefix(contentType, httpclient.ContentTypeApplicationXml) && !strings.HasPrefix(contentType, httpclient.ContentTypeTextXml) {
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var illMessage iso18626.Iso18626MessageNS
		err = xml.Unmarshal(byteReq, &illMessage)
		if err != nil {
			http.Error(w, "unmarshal: "+err.Error(), http.StatusBadRequest)
			return
		}
		if illMessage.Request != nil {
			illRequest := illMessage.Request
			if illRequest.ServiceInfo != nil {
				subtypes := illRequest.ServiceInfo.RequestSubType
				if slices.Contains(subtypes, iso18626.TypeRequestSubTypePatronRequest) {
					app.handlePatronRequest(&illMessage, w)
					return
				}
			}
			app.logIncomingReq(role.Supplier, &illRequest.Header, &illMessage)
			app.handleSupplierRequest(illRequest, w)
		} else if illMessage.RequestingAgencyMessage != nil {
			app.handleIso18626RequestingAgencyMessage(&illMessage, w)
		} else if illMessage.SupplyingAgencyMessage != nil {
			app.handleIso18626SupplyingAgencyMessage(&illMessage, w)
		} else {
			log.Warn("invalid ISO18626 message")
			http.Error(w, "invalid ISO18626 message", http.StatusBadRequest)
			return
		}
	}
}

func getSupplyDuration(val string) (time.Duration, error) {
	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("invalid SUPPLY_DURATION: %s", err.Error())
	}
	if d < 0 {
		return 0, errors.New("SUPPLY_DURATION can not be negative")
	}
	return d, nil
}

func (app *MockApp) parseEnv() error {
	if app.httpPort == "" {
		app.httpPort = utils.GetEnv("HTTP_PORT", "8081")
	}
	if app.agencyType == "" {
		app.agencyType = os.Getenv("AGENCY_TYPE")
	}
	if app.requester.supplyingAgencyId == "" {
		app.requester.supplyingAgencyId = os.Getenv("SUPPLYING_AGENCY_ID")
	}
	if app.requester.requestingAgencyId == "" {
		app.requester.requestingAgencyId = os.Getenv("REQUESTING_AGENCY_ID")
	}
	if app.peerUrl == "" {
		app.peerUrl = utils.GetEnv("PEER_URL", "http://localhost:8081")
	}
	if app.supplyDuration == 0 {
		d, err := getSupplyDuration(utils.GetEnv("SUPPLY_DURATION", "100ms"))
		if err != nil {
			return err
		}
		app.supplyDuration = d
	}
	return nil
}

func (app *MockApp) Shutdown() error {
	if app.flowsApi != nil {
		app.flowsApi.Shutdown()
	}
	if app.server != nil {
		return app.server.Shutdown(context.Background())
	}
	return nil
}

func (app *MockApp) Run() error {
	err := app.parseEnv()
	if err != nil {
		return err
	}
	iso18626.InitNs()
	log.Info("Mock starting")
	if app.agencyType == "" {
		app.agencyType = "MOCK"
	}
	requester := &app.requester
	if requester.requestingAgencyId == "" {
		requester.requestingAgencyId = "REQ"
	}
	if requester.supplyingAgencyId == "" {
		requester.supplyingAgencyId = "SUP"
	}
	addr := app.httpPort
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	if app.flowsApi == nil {
		app.flowsApi = flows.CreateFlowsApi()
		err := app.flowsApi.ParseEnv()
		if err != nil {
			return err
		}
	}
	app.sruApi = sruapi.CreateSruApi()
	log.Info("Start HTTP serve on " + addr)
	mux := http.NewServeMux()
	mux.HandleFunc("/iso18626", iso18626Handler(app))
	mux.HandleFunc("/healthz", healthHandler())
	mux.HandleFunc("/api/flows", app.flowsApi.HttpHandler())
	mux.HandleFunc("/sru", app.sruApi.HttpHandler())
	app.server = &http.Server{Addr: addr, Handler: mux}
	app.flowsApi.Run()
	return app.server.ListenAndServe()
}
