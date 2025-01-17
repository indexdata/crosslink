package app

import (
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

const (
	Supplier  Role = "supplier"
	Requester Role = "requester"
)

type MockApp struct {
	role     Role
	httpPort string
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

func (app *MockApp) parseConfig() error {
	app.httpPort = os.Getenv("HTTP_PORT")
	if app.httpPort == "" {
		app.httpPort = "8081"
	}
	role := os.Getenv("ILLMOCK_ROLE")
	switch strings.ToLower(role) {
	case string(Supplier):
		app.role = Supplier
	case string(Requester):
		app.role = Requester
	case "":
		app.role = Supplier
	default:
		return fmt.Errorf("bad value for ILLMOCK_ROLE")
	}
	return nil
}

func (app *MockApp) Run() error {
	err := app.parseConfig()
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/iso18626", iso18626Handler(app))
	return http.ListenAndServe(":"+app.httpPort, mux)
}
