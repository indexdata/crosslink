package app

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/crosslink/illmock/slogwrap"
	"github.com/indexdata/go-utils/utils"
)

type Role string

type MockApp struct {
	httpPort    string
	isSupplier  bool
	isRequester bool
	remoteUrl   string
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
	w.Write(output)
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
	if illRequest.Header.RequestingAgencyRequestId == "" {
		handleRequestError(illRequest, "Requesting agency request id cannot be empty", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}
	var resmsg = createRequestResponse(illRequest, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w)
}

func (app *MockApp) handleIso18626RequestingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Info("handleIso18626RequestingAgencyMessage")
}

func (app *MockApp) handleIso18626SupplyingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	log.Info("handleIso18626SupplyingAgencyMessage")
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

func httpRequestResponse(client *http.Client, url string, msg *iso18626.ISO18626Message) (*iso18626.ISO18626Message, error) {
	buf, err := xml.Marshal(msg)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url+"/iso18626", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP POST error: %d", resp.StatusCode)
	}
	var response iso18626.ISO18626Message
	err = xml.Unmarshal(body, &response)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

func (app *MockApp) runRequester() {
	slog.Info("requester: initiating")
	time.Sleep(100 * time.Millisecond)
	msg := createRequest()
	responseMsg, err := httpRequestResponse(http.DefaultClient, app.remoteUrl, msg)
	if err != nil {
		slog.Error("requester:", "msg", err.Error())
		return
	}
	slog.Info("requester: OK")
	requestConfirmation := responseMsg.RequestConfirmation
	if requestConfirmation != nil {
		slog.Info("Got requestConfirmation")
	}
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
	return http.ListenAndServe(":"+app.httpPort, mux)
}
