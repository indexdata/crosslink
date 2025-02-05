package app

import (
	"context"
	"encoding/xml"
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
	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
)

type requesterInfo struct {
	action iso18626.TypeAction
}

type Role string

const (
	RoleSupplier  Role = "supplier"
	RoleRequester Role = "requester"
)

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
	httpPort   string
	agencyType string
	peerUrl    string
	server     *http.Server
	requester  Requester
	supplier   Supplier
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

func writeResponse(resmsg *iso18626.Iso18626MessageNS, w http.ResponseWriter, role Role, header *iso18626.Header) {
	buf := utils.Must(xml.MarshalIndent(resmsg, "  ", "  "))
	if buf == nil {
		http.Error(w, "marshal failed", http.StatusInternalServerError)
		return
	}
	logResponse(role, header, resmsg)
	w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
	w.WriteHeader(http.StatusOK)
	_, err := w.Write(buf)
	if err != nil {
		log.Warn("writeResponse", "error", err.Error())
	}
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
	var resmsg = &iso18626.Iso18626MessageNS{}
	header := createConfirmationHeader(requestHeader, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func createRequest() *iso18626.Iso18626MessageNS {
	var msg = &iso18626.Iso18626MessageNS{}
	msg.Request = &iso18626.Request{}
	return msg
}

func handleRequestError(requestHeader *iso18626.Header, role Role, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestResponse(requestHeader, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w, role, requestHeader)
}

func handleRequestingAgencyMessageError(request *iso18626.RequestingAgencyMessage, role Role, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestingAgencyConfirmation(&request.Header, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	resmsg.RequestingAgencyMessageConfirmation.Action = &request.Action
	writeResponse(resmsg, w, role, &request.Header)
}

func (app *MockApp) sendReceive(msg *iso18626.Iso18626MessageNS, role Role, header *iso18626.Header) (*iso18626.ISO18626Message, error) {
	buf := utils.Must(xml.MarshalIndent(msg, "  ", "  "))
	if buf == nil {
		return nil, fmt.Errorf("marshal failed")
	}
	// TODO: should really make a custom error for the SendReceiveXml to get status code out..
	resp, err := httpclient.SendReceiveXml(http.DefaultClient, app.peerUrl+"/iso18626", buf)
	if err != nil {
		logOutgoing(role, header, msg, app.peerUrl+"/iso18626", 500) // TODO: get status code from error
		return nil, err
	}
	logOutgoing(role, header, msg, app.peerUrl+"/iso18626", 200)
	var response iso18626.ISO18626Message
	err = xml.Unmarshal(resp, &response)
	if err != nil {
		return nil, err
	}
	logIncoming(role, header, msg)
	return &response, nil
}

func (app *MockApp) handlePatronRequest(illRequest *iso18626.Request, w http.ResponseWriter) {
	patronReqHeader := illRequest.Header

	requester := &app.requester
	msg := createRequest()
	msg.Request = illRequest // using same Request as received
	header := &illRequest.Header

	msg.Request.ServiceInfo = nil // not a patron request any more

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

	responseMsg, err := app.sendReceive(msg, RoleRequester, header)
	if err != nil {
		slog.Error("requester:", "msg", err.Error())
		errorMessage := fmt.Sprintf("Error sending request to supplier: %s", err.Error())
		handleRequestError(&patronReqHeader, RoleRequester, errorMessage, iso18626.TypeErrorTypeUnrecognisedDataElement, w)
		return
	}
	requestConfirmation := responseMsg.RequestConfirmation
	if requestConfirmation == nil {
		slog.Warn("requester: Did not receive requestConfirmation")
		handleRequestError(&patronReqHeader, RoleRequester, "Did not receive requestConfirmation from supplier", iso18626.TypeErrorTypeUnrecognisedDataElement, w)
		return
	}
	slog.Info("Got requestConfirmation")

	requester.store(header, &requesterInfo{action: iso18626.TypeActionReceived})
	var resmsg = createRequestResponse(&patronReqHeader, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w, RoleRequester, header)
}

func (app *MockApp) handleSupplierRequest(illRequest *iso18626.Request, w http.ResponseWriter) {
	supplier := &app.supplier
	err := validateHeader(&illRequest.Header)
	if err != nil {
		handleRequestError(&illRequest.Header, RoleSupplier, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	v := supplier.load(&illRequest.Header)
	if v != nil {
		handleRequestError(&illRequest.Header, RoleSupplier, "RequestingAgencyRequestId already exists", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
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
	default:
		status = append(status, iso18626.TypeStatusUnfilled)
	}
	supplier.store(&illRequest.Header, &supplierInfo{status: status, index: 0,
		supplierRequestId: uuid.NewString()})

	var resmsg = createRequestResponse(&illRequest.Header, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w, RoleSupplier, &illRequest.Header)
	go app.sendSupplyingAgencyMessage(&illRequest.Header)
}

func logIncoming(role Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS) {
	buf := utils.Must(xml.MarshalIndent(illMessage, "  ", "  "))
	if buf == nil {
		return
	}
	lead := fmt.Sprintf("incoming %s %s %s %s\n%s", role, header.SupplyingAgencyId.AgencyIdValue,
		header.RequestingAgencyId.AgencyIdValue, header.RequestingAgencyRequestId, buf)
	slog.Info(lead)
}

func logOutgoing(role Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS,
	url string, statusCode int) {
	buf := utils.Must(xml.MarshalIndent(illMessage, "  ", "  "))
	if buf == nil {
		return
	}
	lead := fmt.Sprintf("outgoing %s %s %s %s %s %d\n%s", role, header.SupplyingAgencyId.AgencyIdValue,
		header.RequestingAgencyId.AgencyIdValue, header.RequestingAgencyRequestId, url, statusCode, buf)
	slog.Info(lead)
}

func logResponse(role Role, header *iso18626.Header, illMessage *iso18626.Iso18626MessageNS) {
	buf := utils.Must(xml.MarshalIndent(illMessage, "  ", "  "))
	if buf == nil {
		return
	}
	lead := fmt.Sprintf("response %s %s %s %s\n%s", role, header.SupplyingAgencyId.AgencyIdValue,
		header.RequestingAgencyId.AgencyIdValue, header.RequestingAgencyRequestId, buf)
	slog.Info(lead)
}

func (app *MockApp) handleIso18626Request(illMessage *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	illRequest := illMessage.Request

	if illRequest.ServiceInfo != nil {
		subtypes := illRequest.ServiceInfo.RequestSubType
		if slices.Contains(subtypes, iso18626.TypeRequestSubTypePatronRequest) {
			app.handlePatronRequest(illRequest, w)
			return
		}
	}
	logIncoming(RoleSupplier, &illRequest.Header, illMessage)
	app.handleSupplierRequest(illRequest, w)
}

func createSupplyingAgencyMessage() *iso18626.Iso18626MessageNS {
	var msg = &iso18626.Iso18626MessageNS{}
	msg.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
	return msg
}

func (app *MockApp) sendSupplyingAgencyMessage(header *iso18626.Header) {
	time.Sleep(100 * time.Millisecond)
	log.Info("sendSupplyingAgencyMessage")

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
	state.index++
	responseMsg, err := app.sendReceive(msg, RoleSupplier, header)
	if err != nil {
		log.Warn("sendSupplyingAgencyMessage", "error", err.Error())
		return
	}
	if responseMsg.SupplyingAgencyMessageConfirmation == nil {
		log.Warn("sendSupplyingAgencyMessage did not receive SupplyingAgencyMessageConfirmation")
		return
	}
	if state.index < len(state.status) {
		go app.sendSupplyingAgencyMessage(header)
	}
}

func createRequestingAgencyConfirmation(iheader *iso18626.Header, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = &iso18626.Iso18626MessageNS{}
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
	logIncoming(RoleSupplier, &requestingAgencyMessage.Header, illMessage)
	err := validateHeader(&requestingAgencyMessage.Header)
	if err != nil {
		handleRequestingAgencyMessageError(requestingAgencyMessage, RoleSupplier, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	var resmsg = createRequestingAgencyConfirmation(&requestingAgencyMessage.Header, iso18626.TypeMessageStatusOK, nil, nil)
	resmsg.RequestingAgencyMessageConfirmation.Action = &requestingAgencyMessage.Action
	writeResponse(resmsg, w, RoleSupplier, &requestingAgencyMessage.Header)

	if requestingAgencyMessage.Action != iso18626.TypeActionShippedReturn {
		return
	}
	msg := createSupplyingAgencyMessage()
	msg.SupplyingAgencyMessage.Header = requestingAgencyMessage.Header
	header := &requestingAgencyMessage.Header

	supplier := &app.supplier
	state := supplier.load(header)
	if state == nil {
		log.Warn("sendSupplyingAgencyMessage no key", "key", supplier.getKey(header))
		return
	}
	state.index = 0
	state.status = []iso18626.TypeStatus{iso18626.TypeStatusLoanCompleted}
	go app.sendSupplyingAgencyMessage(header)
}

func createSupplyingAgencyResponse(supplyingAgencyMessage *iso18626.SupplyingAgencyMessage, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = &iso18626.Iso18626MessageNS{}
	header := createConfirmationHeader(&supplyingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func handleSupplyingAgencyError(illMessage *iso18626.SupplyingAgencyMessage, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w, RoleRequester, &illMessage.Header)
}

func createRequestingAgencyMessage() *iso18626.Iso18626MessageNS {
	var msg = &iso18626.Iso18626MessageNS{}
	msg.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{}
	return msg
}

func (app *MockApp) handleIso18626SupplyingAgencyMessage(illMessage *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	requester := &app.requester
	supplyingAgencyMessage := illMessage.SupplyingAgencyMessage
	header := &supplyingAgencyMessage.Header
	logIncoming(RoleRequester, header, illMessage)
	err := validateHeader(header)
	if err != nil {
		handleSupplyingAgencyError(supplyingAgencyMessage, err.Error(), iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	v := requester.load(header)
	if v == nil {
		handleSupplyingAgencyError(supplyingAgencyMessage, "Non existing RequestingAgencyRequestId", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	resmsg := createSupplyingAgencyResponse(supplyingAgencyMessage, iso18626.TypeMessageStatusOK, nil, nil)
	reason := iso18626.TypeReasonForMessageRequestResponse
	resmsg.SupplyingAgencyMessageConfirmation.ReasonForMessage = &reason
	writeResponse(resmsg, w, RoleRequester, header)
	if supplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoaned {
		go app.sendRequestingAgencyMessage(header)
	}
	if supplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoanCompleted {
		requester.delete(header)
	}
}

func (app *MockApp) sendRequestingAgencyMessage(header *iso18626.Header) {
	requester := &app.requester
	state := requester.load(header)
	if state == nil {
		log.Warn("sendRequestingAgencyMessage request gone", "key", requester.getKey(header))
		return
	}
	log.Info("sendRequestingAgencyMessage")

	msg := createRequestingAgencyMessage()
	msg.RequestingAgencyMessage.Header = *header
	msg.RequestingAgencyMessage.Action = state.action

	responseMsg, err := app.sendReceive(msg, "requester", header)
	if err != nil {
		log.Warn("sendRequestingAgencyMessage", "error", err.Error())
		return
	}
	if responseMsg.RequestingAgencyMessageConfirmation == nil {
		log.Warn("sendRequestingAgencyMessage did not receive RequestingAgencyMessageConfirmation")
		return
	}
	if responseMsg.RequestingAgencyMessageConfirmation.Action == nil || *responseMsg.RequestingAgencyMessageConfirmation.Action != state.action {
		log.Warn("sendRequestingAgencyMessage did not receive same action in confirmation")
		return
	}
	if state.action == iso18626.TypeActionReceived {
		state.action = iso18626.TypeActionShippedReturn
		go app.sendRequestingAgencyMessage(header)
	}
}

func iso18626Handler(app *MockApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			log.Info("[iso18626-handler] error: method not allowed", "method", r.Method, "url", r.URL)
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get(httpclient.ContentType)
		if !strings.HasPrefix(contentType, httpclient.ContentTypeApplicationXml) && !strings.HasPrefix(contentType, httpclient.ContentTypeTextXml) {
			log.Info("[iso18626-handler] error: content-type unsupported", httpclient.ContentType, contentType, "url", r.URL)
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			log.Info("[iso18626-handler] error: failure reading request: ", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var illMessage iso18626.Iso18626MessageNS
		err = xml.Unmarshal(byteReq, &illMessage)
		if err != nil {
			log.Info("[iso18626-handler] error: unmarshal", "error", err)
			http.Error(w, "unmarshal: "+err.Error(), http.StatusBadRequest)
			return
		}
		if illMessage.Request != nil {
			app.handleIso18626Request(&illMessage, w)
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

func (app *MockApp) parseConfig() {
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
}

func (app *MockApp) Shutdown() error {
	if app.server != nil {
		return app.server.Shutdown(context.Background())
	}
	return nil
}

func (app *MockApp) Run() error {
	app.parseConfig()
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
	log.Info("Start HTTP serve on " + addr)
	mux := http.NewServeMux()
	mux.HandleFunc("/iso18626", iso18626Handler(app))
	app.server = &http.Server{Addr: addr, Handler: mux}
	// both requester and responder serves HTTP
	return app.server.ListenAndServe()
}
