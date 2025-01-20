package app

import (
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/crosslink/illmock/http18626"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/go-utils/utils"
)

type Role string

type state struct {
	index             int
	status            []iso18626.TypeStatus
	supplierRequestId string
}

type MockApp struct {
	httpPort    string
	isSupplier  bool
	isRequester bool
	remoteUrl   string
	requestId   map[string]*state
	server      *http.Server
}

var log *slog.Logger = slogwrap.SlogWrap()

func writeResponse(resmsg *iso18626.ISO18626Message, w http.ResponseWriter) {
	output, err := xml.MarshalIndent(resmsg, "  ", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/xml")
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

func createRequestResponse(illRequest *iso18626.Request, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&illRequest.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func createRequest() *iso18626.ISO18626Message {
	var msg = &iso18626.ISO18626Message{}
	msg.Request = &iso18626.Request{}
	return msg
}

func handleRequestError(illRequest *iso18626.Request, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestResponse(illRequest, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w)
}

func (app *MockApp) handleIso18626Request(illRequest *iso18626.Request, w http.ResponseWriter) {
	log.Info("handleIso18626Request")
	if !app.isSupplier {
		handleRequestError(illRequest, "Only supplier expects ISO18626 Request", iso18626.TypeErrorTypeUnsupportedActionType, w)
	}
	if illRequest.Header.RequestingAgencyRequestId == "" {
		handleRequestError(illRequest, "Requesting agency request id cannot be empty", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	// TODO: check if illRequest.Header.SupplyingAgencyRequestId == ""

	_, ok := app.requestId[illRequest.Header.RequestingAgencyRequestId]
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
	app.requestId[illRequest.Header.RequestingAgencyRequestId] = &state{status: status, index: 0,
		supplierRequestId: "S" + uuid.NewString()}

	var resmsg = createRequestResponse(illRequest, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w)
	go app.sendSupplyingAgencyMessage(&illRequest.Header)
}

func createSupplyingAgencyMessage() *iso18626.ISO18626Message {
	var msg = &iso18626.ISO18626Message{}
	msg.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{}
	return msg
}

func (app *MockApp) sendSupplyingAgencyMessage(header *iso18626.Header) {
	time.Sleep(500 * time.Millisecond)
	log.Info("sendSupplyingAgencyMessage")

	msg := createSupplyingAgencyMessage()
	msg.SupplyingAgencyMessage.Header = *header

	state, ok := app.requestId[header.RequestingAgencyRequestId]
	if !ok {
		log.Warn("sendSupplyingAgencyMessage no state", "id", header.RequestingAgencyRequestId)
		return
	}
	msg.SupplyingAgencyMessage.Header.SupplyingAgencyRequestId = state.supplierRequestId
	msg.SupplyingAgencyMessage.StatusInfo.Status = state.status[state.index]
	state.index++
	responseMsg, err := http18626.SendReceiveDefault(app.remoteUrl, msg)
	if err != nil {
		log.Warn("sendSupplyingAgencyMessage", "error", err.Error())
		return
	}
	if responseMsg.RequestingAgencyMessageConfirmation == nil {
		log.Warn("sendSupplyingAgencyMessage did not receive RequestingAgencyMessageConfirmation")
		return
	}
	if state.index < len(state.status) {
		go app.sendSupplyingAgencyMessage(header)
	}
}

func (app *MockApp) handleIso18626RequestingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Info("handleIso18626RequestingAgencyMessage")
}

func createSupplyingAgencyResponse(illMessage *iso18626.ISO18626Message, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&illMessage.SupplyingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func handleSupplyingAgencyError(illMessage *iso18626.ISO18626Message, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w)
}

func (app *MockApp) handleIso18626SupplyingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Info("handleIso18626SupplyingAgencyMessage")
	if !app.isRequester {
		handleSupplyingAgencyError(illMessage, "Only requester expects ISO18626 SupplyingAgencyMessage", iso18626.TypeErrorTypeUnsupportedActionType, w)
		return
	}
	resmsg := createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w)
}

func iso18626Handler(app *MockApp) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			log.Info("[iso18626-handler] error: method not allowed", "method", r.Method, "url", r.URL)
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/xml") && !strings.HasPrefix(contentType, "text/xml") {
			log.Info("[iso18626-handler] error: content-type unsupported", "contentType", contentType, "url", r.URL)
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			log.Info("[iso18626-server] error: failure reading request: ", "error", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var illMessage iso18626.ISO18626Message
		err = xml.Unmarshal(byteReq, &illMessage)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if illMessage.Request != nil {
			app.handleIso18626Request(illMessage.Request, w)
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

func (app *MockApp) runRequester() {
	slog.Info("requester: initiating")
	time.Sleep(100 * time.Millisecond)
	msg := createRequest()
	header := &msg.Request.Header
	header.RequestingAgencyRequestId = "R" + uuid.NewString()
	header.RequestingAgencyId.AgencyIdType.Text = "MOCK"
	header.RequestingAgencyId.AgencyIdValue = "WILLSUPPLY_LOANED"
	responseMsg, err := http18626.SendReceiveDefault(app.remoteUrl, msg)
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
	if app.httpPort == "" {
		app.httpPort = "8081"
	}
	role := strings.ToLower(os.Getenv("ROLE"))
	if role == "" || strings.Contains(role, "supplier") {
		app.isSupplier = true
	}
	if strings.Contains(role, "requester") {
		app.isRequester = true
	}
	app.remoteUrl = os.Getenv("REMOTE_URL")
	if app.remoteUrl == "" {
		app.remoteUrl = "http://localhost:8081"
	}
	return nil
}

func (app *MockApp) Shutdown() error {
	if app.server != nil {
		return app.server.Shutdown(context.TODO())
	}
	return nil
}

func (app *MockApp) Run() error {
	err := app.parseConfig()
	if err != nil {
		return err
	}
	log.Info("Mock starting", "requester", app.isRequester, "supplier", app.isSupplier)
	mux := http.NewServeMux()
	mux.HandleFunc("/iso18626", iso18626Handler(app))
	// it would be great if we could ensure that Requester only be started if ListenAndServe succeeded
	if app.isRequester {
		go app.runRequester()
	}
	app.server = &http.Server{Addr: ":" + app.httpPort}
	// both requester and responder serves HTTP
	return app.server.ListenAndServe()
}
