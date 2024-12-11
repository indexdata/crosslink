package handler

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/db"
	queries "github.com/indexdata/crosslink/broker/db/generated"
	"github.com/indexdata/crosslink/broker/db/model"
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func Iso18626PostHandler(repo repository.Repository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			log.Printf("[iso18626-handler] error: method not allowed: %s %s\n", r.Method, r.URL)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
			HandleIso18626Request(illMessage, w, repo)
		} else if illMessage.RequestingAgencyMessage != nil {
			HandleIso18626RequestingAgencyMessage(illMessage, w)
		} else if illMessage.SupplyingAgencyMessage != nil {
			HandleIso18626SupplyingAgencyMessage(illMessage, w)
		} else {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
}

func HandleIso18626Request(illMessage iso18626.ISO18626Message, w http.ResponseWriter, repo repository.Repository) {
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

	jsonBytes, err := json.Marshal(illTransactionData)
	if err != nil {
		fmt.Println("Error converting map to JSON:", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := context.Background()
	_, err = repo.CreateIllTransaction(ctx, queries.CreateIllTransactionParams{
		ID: uuid.New().String(),
		Timestamp: pgtype.Timestamp{
			Time:  illMessage.Request.Header.Timestamp.Time,
			Valid: true,
		},
		RequesterSymbol:    requesterSymbol,
		RequesterAction:    requestAction,
		SupplierSymbol:     supplierSymbol,
		State:              state,
		RequesterRequestID: requesterRequestId,
		SupplierRequestID:  supplierRequestId,
		Data:               jsonBytes,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var resmsg = createRequestResponse(illMessage)
	output, err := xml.MarshalIndent(resmsg, "  ", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", "application/xml")
	w.Write(output)
}

func createPgText(value string) pgtype.Text {
	textValue := pgtype.Text{}
	textValue.Scan(value)
	return textValue
}

func createRequestResponse(illMessage iso18626.ISO18626Message) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{}

	resmsg.RequestConfirmation.ConfirmationHeader.RequestingAgencyId = &iso18626.TypeAgencyId{}
	resmsg.RequestConfirmation.ConfirmationHeader.RequestingAgencyId.AgencyIdType = illMessage.Request.Header.RequestingAgencyId.AgencyIdType
	resmsg.RequestConfirmation.ConfirmationHeader.RequestingAgencyId.AgencyIdValue = illMessage.Request.Header.RequestingAgencyId.AgencyIdValue
	resmsg.RequestConfirmation.ConfirmationHeader.TimestampReceived = illMessage.Request.Header.Timestamp
	resmsg.RequestConfirmation.ConfirmationHeader.RequestingAgencyRequestId = illMessage.Request.Header.RequestingAgencyRequestId

	if len(illMessage.Request.Header.SupplyingAgencyId.AgencyIdValue) != 0 {
		resmsg.RequestConfirmation.ConfirmationHeader.SupplyingAgencyId = &iso18626.TypeAgencyId{}
		resmsg.RequestConfirmation.ConfirmationHeader.SupplyingAgencyId.AgencyIdType = illMessage.Request.Header.SupplyingAgencyId.AgencyIdType
		resmsg.RequestConfirmation.ConfirmationHeader.SupplyingAgencyId.AgencyIdValue = illMessage.Request.Header.SupplyingAgencyId.AgencyIdValue
	}

	resmsg.RequestConfirmation.ConfirmationHeader.Timestamp = utils.XSDDateTime{Time: time.Now()}
	resmsg.RequestConfirmation.ConfirmationHeader.MessageStatus = iso18626.TypeMessageStatusOK
	return resmsg
}

func HandleIso18626RequestingAgencyMessage(illMessage iso18626.ISO18626Message, w http.ResponseWriter) {

}

func HandleIso18626SupplyingAgencyMessage(illMessage iso18626.ISO18626Message, w http.ResponseWriter) {

}
