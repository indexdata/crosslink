package handler

import (
	"encoding/xml"
	"github.com/indexdata/crosslink/broker/event"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	repository "github.com/indexdata/crosslink/broker/db"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/db/model"
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
)

func Iso18626PostHandler(repo repository.Repository, eventBus event.EventBus) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			log.Printf("[iso18626-handler] error: method not allowed: %s %s\n", r.Method, r.URL)
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/xml") && !strings.HasPrefix(contentType, "text/xml") {
			log.Printf("[iso18626-handler] error: content-type unsupported: %s %s\n", contentType, r.URL)
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			log.Println("[iso18626-server] error: failure reading request: ", err)
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
			handleIso18626Request(&illMessage, w, repo, eventBus)
		} else if illMessage.RequestingAgencyMessage != nil {
			handleIso18626RequestingAgencyMessage(&illMessage, w, repo, eventBus)
		} else if illMessage.SupplyingAgencyMessage != nil {
			handleIso18626SupplyingAgencyMessage(&illMessage, w, repo, eventBus)
		} else {
			http.Error(w, "invalid ISO18626 message", http.StatusBadRequest)
			return
		}
	}
}

func handleIso18626Request(illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo repository.Repository, eventBus event.EventBus) {
	if illMessage.Request.Header.RequestingAgencyRequestId == "" {
		handleRequestError(illMessage, "Requesting agency request id cannot be empty", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}

	requesterSymbol := createPgText(illMessage.Request.Header.RequestingAgencyId.AgencyIdType.Text + ":" + illMessage.Request.Header.RequestingAgencyId.AgencyIdValue)
	supplierSymbol := createPgText(illMessage.Request.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + illMessage.Request.Header.SupplyingAgencyId.AgencyIdValue)
	requestAction := createPgText("Request")
	state := createPgText("NEW")
	requesterRequestId := createPgText(illMessage.Request.Header.RequestingAgencyRequestId)
	supplierRequestId := createPgText(illMessage.Request.Header.SupplyingAgencyRequestId)

	illTransactionData := model.IllTransactionData{
		BibliographicInfo:     illMessage.Request.BibliographicInfo,
		PublicationInfo:       illMessage.Request.PublicationInfo,
		ServiceInfo:           illMessage.Request.ServiceInfo,
		SupplierInfo:          illMessage.Request.SupplierInfo,
		RequestedDeliveryInfo: illMessage.Request.RequestedDeliveryInfo,
		RequestingAgencyInfo:  illMessage.Request.RequestingAgencyInfo,
		PatronInfo:            illMessage.Request.PatronInfo,
		BillingInfo:           illMessage.Request.BillingInfo,
	}

	id := uuid.New().String()
	timestamp := pgtype.Timestamp{
		Time:  illMessage.Request.Header.Timestamp.Time,
		Valid: true,
	}
	_, err := repo.CreateIllTransaction(queries.CreateIllTransactionParams{
		ID:                 id,
		Timestamp:          timestamp,
		RequesterSymbol:    requesterSymbol,
		RequesterAction:    requestAction,
		SupplierSymbol:     supplierSymbol,
		State:              state,
		RequesterRequestID: requesterRequestId,
		SupplierRequestID:  supplierRequestId,
		IllTransactionData: illTransactionData,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	eventData := model.EventData{
		Timestamp:       getNow(),
		ISO18626Message: illMessage,
	}
	err = eventBus.CreateTask(id, model.EventNameRequestReceived, eventData)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resmsg = createRequestResponse(illMessage, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w)
}

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

func handleRequestError(illMessage *iso18626.ISO18626Message, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w)
}

func createPgText(value string) pgtype.Text {
	textValue := pgtype.Text{
		String: value,
		Valid:  true,
	}
	return textValue
}

func createRequestResponse(illMessage *iso18626.ISO18626Message, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&illMessage.Request.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
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

func handleIso18626RequestingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo repository.Repository, eventBus event.EventBus) {
	var requestingRequestId = illMessage.RequestingAgencyMessage.Header.RequestingAgencyRequestId
	if requestingRequestId == "" {
		handleRequestingAgencyError(illMessage, "Missing requesting agency request it", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}

	var illTrans, err = repo.GetIllTransactionByRequesterRequestId(createPgText(requestingRequestId))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if illTrans.ID == "" {
		handleRequestingAgencyError(illMessage, "Could not find ill transaction", iso18626.TypeErrorTypeUnrecognisedDataValue, w)
		return
	}

	eventData := model.EventData{
		Timestamp:       getNow(),
		ISO18626Message: illMessage,
	}
	err = eventBus.CreateNotice(illTrans.ID, model.EventNameRequesterMsgReceived, eventData, model.EventStatusSuccess)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resmsg = createRequestingAgencyResponse(illMessage, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w)
}

func createRequestingAgencyResponse(illMessage *iso18626.ISO18626Message, messageStatus iso18626.TypeMessageStatus, errorMessage *string, errorType *iso18626.TypeErrorType) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&illMessage.RequestingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorMessage, errorType)
	resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func handleRequestingAgencyError(illMessage *iso18626.ISO18626Message, errorMessage string, errorType iso18626.TypeErrorType, w http.ResponseWriter) {
	var resmsg = createRequestingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorMessage, &errorType)
	writeResponse(resmsg, w)
}

func handleIso18626SupplyingAgencyMessage(illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo repository.Repository, eventBus event.EventBus) {
	var requestingRequestId = illMessage.SupplyingAgencyMessage.Header.RequestingAgencyRequestId
	if requestingRequestId == "" {
		handleSupplyingAgencyError(illMessage, "Missing requesting agency request it", iso18626.TypeErrorTypeBadlyFormedMessage, w)
		return
	}

	var illTrans, err = repo.GetIllTransactionByRequesterRequestId(createPgText(requestingRequestId))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if illTrans.ID == "" {
		handleSupplyingAgencyError(illMessage, "Could not find ill transaction", iso18626.TypeErrorTypeBadlyFormedMessage, w)
		return
	}

	eventData := model.EventData{
		Timestamp:       getNow(),
		ISO18626Message: illMessage,
	}
	err = eventBus.CreateNotice(illTrans.ID, model.EventNameSupplierMsgReceived, eventData, model.EventStatusSuccess)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusOK, nil, nil)
	writeResponse(resmsg, w)
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

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
