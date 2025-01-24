package app

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/crosslink/illmock/httpclient"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/go-utils/utils"
)

type Role string

type requesterInfo struct {
	action iso18626.TypeAction
}

type Requester struct {
	requestingAgencyId string
	supplyingAgencyIds []string
	requests           sync.Map // key is requesting agency request id
}

type supplierInfo struct {
	index             int                   // index into status below
	status            []iso18626.TypeStatus // the status that the supplier will return
	supplierRequestId string                // supplier request Id
}

type Supplier struct {
	requests sync.Map // key is requesting agency request id
}

type MockApp struct {
	httpPort   string
	agencyType string
	remoteUrl  string
	server     *http.Server
	requester  *Requester
	supplier   *Supplier
}

var log *slog.Logger = slogwrap.SlogWrap()

func writeResponse(resmsg *iso18626.Iso18626MessageNS, w http.ResponseWriter) {
	output, err := xml.MarshalIndent(resmsg, "  ", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set(httpclient.ContentType, httpclient.ContentTypeApplicationXml)
	w.WriteHeader(http.StatusOK)
	_, err = w.Write(output)
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

func createRequestResponse(illRequest *iso18626.Request, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = &iso18626.Iso18626MessageNS{}
	header := createConfirmationHeader(&illRequest.Header, messageStatus)
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

func handleRequestError(illRequest *iso18626.Request, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestResponse(illRequest, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w)
}

func (app *MockApp) handleIso18626Request(illRequest *iso18626.Request, w http.ResponseWriter) {
	log.Info("handleIso18626Request")
	supplier := app.supplier
	if supplier == nil {
		handleRequestError(illRequest, "Only supplier expects ISO18626 Request", iso18626.TypeErrorTypeUnsupportedActionType, w)
	}
	if illRequest.Header.RequestingAgencyRequestId == "" {
		handleRequestError(illRequest, "Requesting agency request id cannot be empty", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	// TODO: check if illRequest.Header.SupplyingAgencyRequestId == ""

	_, ok := supplier.requests.Load(illRequest.Header.RequestingAgencyRequestId)
	if ok {
		handleRequestError(illRequest, "RequestingAgencyRequestId already exists", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	var status []iso18626.TypeStatus

	// should be able to parse the value and put any types into status...
	switch illRequest.Header.SupplyingAgencyId.AgencyIdValue {
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
	supplier.requests.Store(illRequest.Header.RequestingAgencyRequestId, &supplierInfo{status: status, index: 0,
		supplierRequestId: uuid.NewString()})

	var resmsg = createRequestResponse(illRequest, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w)
	go app.sendSupplyingAgencyMessage(&illRequest.Header)
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

	supplier := app.supplier
	v, ok := supplier.requests.Load(header.RequestingAgencyRequestId)
	if !ok {
		log.Warn("sendSupplyingAgencyMessage no state", "id", header.RequestingAgencyRequestId)
		return
	}
	state := v.(*supplierInfo)
	msg.SupplyingAgencyMessage.Header.SupplyingAgencyRequestId = state.supplierRequestId
	msg.SupplyingAgencyMessage.StatusInfo.Status = state.status[state.index]
	state.index++
	responseMsg, err := httpclient.SendReceiveDefault(app.remoteUrl, msg)
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

func createRequestingAgencyConfirmation(requestingAgencyMessage *iso18626.RequestingAgencyMessage, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.Iso18626MessageNS {
	var resmsg = &iso18626.Iso18626MessageNS{}
	header := createConfirmationHeader(&requestingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func handleRequestingAgencyError(illMessage *iso18626.RequestingAgencyMessage, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestingAgencyConfirmation(illMessage, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w)
}

func (app *MockApp) handleIso18626RequestingAgencyMessage(requestingAgencyMessage *iso18626.RequestingAgencyMessage, w http.ResponseWriter) {
	log.Info("handleIso18626RequestingAgencyMessage")
	supplier := app.supplier
	if supplier == nil {
		handleRequestingAgencyError(requestingAgencyMessage, "Only supplier expects ISO18626 RequestingAgencyMessage", iso18626.TypeErrorTypeUnsupportedActionType, w)
		return
	}
	var resmsg = createRequestingAgencyConfirmation(requestingAgencyMessage, iso18626.TypeMessageStatusOK, nil, nil)
	resmsg.RequestingAgencyMessageConfirmation.Action = &requestingAgencyMessage.Action
	writeResponse(resmsg, w)
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
	writeResponse(resmsg, w)
}

func createRequestingAgencyMessage() *iso18626.Iso18626MessageNS {
	var msg = &iso18626.Iso18626MessageNS{}
	msg.RequestingAgencyMessage = &iso18626.RequestingAgencyMessage{}
	return msg
}

func (app *MockApp) handleIso18626SupplyingAgencyMessage(supplyingAgencyMessage *iso18626.SupplyingAgencyMessage, w http.ResponseWriter) {
	log.Info("handleIso18626SupplyingAgencyMessage")
	requester := app.requester
	if requester == nil {
		handleSupplyingAgencyError(supplyingAgencyMessage, "Only requester expects ISO18626 SupplyingAgencyMessage", iso18626.TypeErrorTypeUnsupportedActionType, w)
		return
	}
	header := &supplyingAgencyMessage.Header
	_, ok := requester.requests.Load(header.RequestingAgencyRequestId)
	if !ok {
		handleSupplyingAgencyError(supplyingAgencyMessage, "Non existing RequestingAgencyRequestId", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}

	resmsg := createSupplyingAgencyResponse(supplyingAgencyMessage, iso18626.TypeMessageStatusOK, nil, nil)
	reason := iso18626.TypeReasonForMessageRequestResponse
	resmsg.SupplyingAgencyMessageConfirmation.ReasonForMessage = &reason
	writeResponse(resmsg, w)
	if supplyingAgencyMessage.StatusInfo.Status == iso18626.TypeStatusLoaned {
		go app.sendRequestingAgencyMessage(header)
	}
}

func (app *MockApp) sendRequestingAgencyMessage(header *iso18626.Header) {
	requester := app.requester
	v, ok := requester.requests.Load(header.RequestingAgencyRequestId)
	if !ok {
		return
	}
	state := v.(*requesterInfo)
	log.Info("sendRequestingAgencyMessage")

	msg := createRequestingAgencyMessage()
	msg.RequestingAgencyMessage.Header = *header
	msg.RequestingAgencyMessage.Action = state.action

	responseMsg, err := httpclient.SendReceiveDefault(app.remoteUrl, msg)
	if err != nil {
		log.Warn("sendRequestingAgencyMessage", "error", err.Error())
		return
	}
	if responseMsg.RequestingAgencyMessageConfirmation == nil {
		log.Warn("sendRequestingAgencyMessage did not receive RequestingAgencyMessageConfirmation")
		return
	}
	if *responseMsg.RequestingAgencyMessageConfirmation.Action != state.action {
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
			app.handleIso18626Request(illMessage.Request, w)
		} else if illMessage.RequestingAgencyMessage != nil {
			app.handleIso18626RequestingAgencyMessage(illMessage.RequestingAgencyMessage, w)
		} else if illMessage.SupplyingAgencyMessage != nil {
			app.handleIso18626SupplyingAgencyMessage(illMessage.SupplyingAgencyMessage, w)
		} else {
			log.Warn("invalid ISO18626 message")
			http.Error(w, "invalid ISO18626 message", http.StatusBadRequest)
			return
		}
	}
}

func (app *MockApp) runRequester(agencyId string) {
	requester := app.requester
	slog.Info("requester: initiating")
	time.Sleep(100 * time.Millisecond)
	msg := createRequest()
	header := &msg.Request.Header
	header.RequestingAgencyRequestId = uuid.NewString()

	requester.requests.Store(header.RequestingAgencyRequestId, &requesterInfo{action: iso18626.TypeActionReceived})
	header.RequestingAgencyId.AgencyIdType.Text = app.agencyType
	header.RequestingAgencyId.AgencyIdValue = requester.requestingAgencyId
	header.SupplyingAgencyId.AgencyIdType.Text = app.agencyType
	header.SupplyingAgencyId.AgencyIdValue = agencyId
	responseMsg, err := httpclient.SendReceiveDefault(app.remoteUrl, msg)
	if err != nil {
		slog.Error("requester:", "msg", err.Error())
		return
	}
	requestConfirmation := responseMsg.RequestConfirmation
	if requestConfirmation == nil {
		slog.Warn("requester: Did not receive requestConfirmation")
		return
	}

	slog.Info("Got requestConfirmation")
}

func (app *MockApp) parseConfig() error {
	app.httpPort = os.Getenv("HTTP_PORT")
	role := strings.ToLower(os.Getenv("ROLE"))
	if role == "" || strings.Contains(role, "supplier") {
		app.supplier = &Supplier{}
	}
	reqEnv := os.Getenv("REQUESTER_SUPPLY_IDS")
	if reqEnv != "" {
		app.requester = &Requester{supplyingAgencyIds: strings.Split(reqEnv, ",")}
	}
	app.remoteUrl = os.Getenv("REMOTE_URL")
	if app.remoteUrl == "" {
		app.remoteUrl = "http://localhost:8081"
	}
	return nil
}

func (app *MockApp) Shutdown() error {
	if app.server != nil {
		return app.server.Shutdown(context.Background())
	}
	return nil
}

func (app *MockApp) Run() error {
	err := app.parseConfig()
	if err != nil {
		return err
	}
	if app.agencyType == "" {
		app.agencyType = "MOCK"
	}
	iso18626.InitNs()
	log.Info("Mock starting", "requester", app.requester != nil, "supplier", app.supplier != nil)
	// it would be great if we could ensure that Requester only be started if ListenAndServe succeeded
	requester := app.requester
	if requester != nil {
		if requester.requestingAgencyId == "" {
			requester.requestingAgencyId = "REQ"
		}
		for _, id := range requester.supplyingAgencyIds {
			go app.runRequester(id)
		}
	}
	if app.httpPort == "" {
		app.httpPort = "8081"
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
