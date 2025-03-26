package handler

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"

	"github.com/google/uuid"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type ErrorValue string

const (
	ReqIdAlreadyExists        ErrorValue = "requestingAgencyRequestId: request with a given ID already exists"
	ReqIdIsEmpty              ErrorValue = "requestingAgencyRequestId: cannot be empty"
	ReqIdNotFound             ErrorValue = "requestingAgencyRequestId: request with a given ID not found"
	NoRetryableIllTransaction ErrorValue = "no retryable ILL transaction"
	UnsupportedRequestType    ErrorValue = "unsupported request type"
	SuppUniqueRecIdIsEmpty    ErrorValue = "supplierUniqueRecordId: cannot be empty"
	ReqAgencyNotFound         ErrorValue = "requestingAgencyId: requesting agency not found"
	CouldNotSendReqToPeer     ErrorValue = "Could not send request to peer"
	InvalidAction             ErrorValue = "%v is not a valid action"
	InvalidStatus             ErrorValue = "%v is not a valid status"
	InvalidReason             ErrorValue = "%v is not a valid reason"
)

const PublicFailedToProcessReqMsg = "failed to process request"
const InternalFailedToLookupTx = "failed to lookup ILL transaction"
const InternalFailedToSaveTx = "failed to save ILL transaction"
const InternalFailedToCreateNotice = "failed to create notice event"

var requestMapping = map[string]RequestWait{}

type Iso18626Handler struct {
	eventBus  events.EventBus
	eventRepo events.EventRepo
}

func CreateIso18626Handler(eventBus events.EventBus, eventRepo events.EventRepo) Iso18626Handler {
	return Iso18626Handler{
		eventBus:  eventBus,
		eventRepo: eventRepo,
	}
}

func Iso18626PostHandler(repo ill_db.IllRepo, eventBus events.EventBus, dirAdapter adapter.DirectoryLookupAdapter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := extctx.CreateExtCtxWithArgs(r.Context(), &extctx.LoggerArgs{RequestId: uuid.NewString()})
		if r.Method != http.MethodPost {
			ctx.Logger().Error("method not allowed", "method", r.Method, "url", r.URL)
			http.Error(w, "only POST allowed", http.StatusMethodNotAllowed)
			return
		}
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "application/xml") && !strings.HasPrefix(contentType, "text/xml") {
			ctx.Logger().Error("content-type unsupported", "contentType", contentType, "url", r.URL)
			http.Error(w, "only application/xml or text/xml accepted", http.StatusUnsupportedMediaType)
			return
		}
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			ctx.Logger().Error("failure reading request", "error", err, "body", string(byteReq))
			http.Error(w, "failure reading request", http.StatusBadRequest)
			return
		}
		var illMessage iso18626.ISO18626Message
		err = xml.Unmarshal(byteReq, &illMessage)
		if err != nil {
			ctx.Logger().Error("failure parsing request", "error", err, "body", string(byteReq))
			http.Error(w, "failure parsing request", http.StatusBadRequest)
			return
		}

		if illMessage.Request != nil {
			handleIso18626Request(ctx, &illMessage, w, repo, eventBus, dirAdapter)
		} else if illMessage.RequestingAgencyMessage != nil {
			handleIso18626RequestingAgencyMessage(ctx, &illMessage, w, repo, eventBus)
		} else if illMessage.SupplyingAgencyMessage != nil {
			handleIso18626SupplyingAgencyMessage(ctx, &illMessage, w, repo, eventBus)
		} else {
			ctx.Logger().Error("invalid ISO18626 message", "error", err, "body", string(byteReq))
			http.Error(w, "invalid ISO18626 message", http.StatusBadRequest)
			return
		}
	}
}

func handleNewRequest(ctx extctx.ExtendedContext, request *iso18626.Request, repo ill_db.IllRepo, requesterSymbol pgtype.Text, peers []ill_db.Peer) (string, error) {
	supplierSymbol := createPgText(request.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + request.Header.SupplyingAgencyId.AgencyIdValue)
	requesterRequestId := createPgText(request.Header.RequestingAgencyRequestId)
	supplierRequestId := createPgText(request.Header.SupplyingAgencyRequestId)

	illTransactionData := ill_db.IllTransactionData{
		BibliographicInfo:     request.BibliographicInfo,
		PublicationInfo:       request.PublicationInfo,
		ServiceInfo:           request.ServiceInfo,
		SupplierInfo:          request.SupplierInfo,
		RequestedDeliveryInfo: request.RequestedDeliveryInfo,
		RequestingAgencyInfo:  request.RequestingAgencyInfo,
		PatronInfo:            request.PatronInfo,
		BillingInfo:           request.BillingInfo,
	}

	id := uuid.New().String()
	timestamp := pgtype.Timestamp{
		Time:  request.Header.Timestamp.Time,
		Valid: true,
	}
	_, err := repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{
		ID:                  id,
		Timestamp:           timestamp,
		RequesterSymbol:     requesterSymbol,
		RequesterID:         createPgText(peers[0].ID),
		LastRequesterAction: createPgText("Request"),
		SupplierSymbol:      supplierSymbol,
		RequesterRequestID:  requesterRequestId,
		SupplierRequestID:   supplierRequestId,
		IllTransactionData:  illTransactionData,
	})
	return id, err
}

func handleRetryRequest(ctx extctx.ExtendedContext, request *iso18626.Request, repo ill_db.IllRepo) (string, error) {
	// ServiceInfo already nil checked in handleIso18626Request
	previusRequestId := request.ServiceInfo.RequestingAgencyPreviousRequestId

	var id string
	err := repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		illTrans, err := repo.GetIllTransactionByRequesterRequestIdForUpdate(ctx, createPgText(previusRequestId))
		if err != nil {
			return err
		}
		selSup, err := repo.GetSelectedSupplierForIllTransaction(ctx, illTrans.ID)
		if err != nil {
			return err
		}
		if selSup.LastStatus.String != string(iso18626.TypeStatusRetryPossible) {
			return errors.New("lastStatus is not RetryPossible")
		}
		requesterRequestId := createPgText(request.Header.RequestingAgencyRequestId)
		illTrans.RequesterRequestID = requesterRequestId
		id = illTrans.ID
		// set previous requester request ID here

		illTrans.LastRequesterAction = createPgText("Request")

		illTransactionData := ill_db.IllTransactionData{
			BibliographicInfo:     request.BibliographicInfo,
			PublicationInfo:       request.PublicationInfo,
			ServiceInfo:           request.ServiceInfo,
			SupplierInfo:          request.SupplierInfo,
			RequestedDeliveryInfo: request.RequestedDeliveryInfo,
			RequestingAgencyInfo:  request.RequestingAgencyInfo,
			PatronInfo:            request.PatronInfo,
			BillingInfo:           request.BillingInfo,
		}
		illTrans.IllTransactionData = illTransactionData

		timestamp := pgtype.Timestamp{
			Time:  request.Header.Timestamp.Time,
			Valid: true,
		}
		illTrans.Timestamp = timestamp

		_, err = repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTrans))
		return err
	})
	return id, err
}

func handleIso18626Request(ctx extctx.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo ill_db.IllRepo, eventBus events.EventBus, dirAdapter adapter.DirectoryLookupAdapter) {
	request := illMessage.Request
	if request.Header.RequestingAgencyRequestId == "" {
		handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdIsEmpty)
		return
	}

	if request.BibliographicInfo.SupplierUniqueRecordId == "" {
		handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, SuppUniqueRecIdIsEmpty)
		return
	}

	requesterSymbol := createPgText(request.Header.RequestingAgencyId.AgencyIdType.Text + ":" + request.Header.RequestingAgencyId.AgencyIdValue)
	peers := repo.GetCachedPeersBySymbols(ctx, []string{requesterSymbol.String}, dirAdapter)
	if len(peers) != 1 {
		handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqAgencyNotFound)
		return
	}

	var err error
	var id string
	var event events.EventName

	requestType := iso18626.TypeRequestTypeNew
	if request.ServiceInfo != nil && request.ServiceInfo.RequestType != nil {
		requestType = *request.ServiceInfo.RequestType
	}
	switch requestType {
	case iso18626.TypeRequestTypeRetry:
		event = events.EventNameRequesterMsgReceived
		id, err = handleRetryRequest(ctx, request, repo)
	case iso18626.TypeRequestTypeNew:
		event = events.EventNameRequestReceived
		id, err = handleNewRequest(ctx, request, repo, requesterSymbol, peers)
	default:
		handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, UnsupportedRequestType)
		return
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgerrcode.IsIntegrityConstraintViolation(pgErr.Code) {
			handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdAlreadyExists)
		} else if errors.Is(err, pgx.ErrNoRows) {
			ctx.Logger().Error(InternalFailedToLookupTx, "error", err)
			handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, NoRetryableIllTransaction)
		} else {
			ctx.Logger().Error(InternalFailedToSaveTx, "error", err)
			http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		}
		return
	}
	var resmsg = createRequestResponse(request, iso18626.TypeMessageStatusOK, nil, "")
	eventData := events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: illMessage,
			OutgoingMessage: resmsg,
		},
	}
	if createNoticeAndCheckDBError(ctx, w, eventBus, id, event, eventData, events.EventStatusSuccess) == "" {
		return
	}
	writeResponse(ctx, resmsg, w)
}

func writeResponse(ctx extctx.ExtendedContext, resmsg *iso18626.ISO18626Message, w http.ResponseWriter) {
	output, err := xml.MarshalIndent(resmsg, "  ", "  ")
	if err != nil {
		ctx.Logger().Error("failed to produce response", "error", err, "body", string(output))
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	w.Write(output)
}

func handleRequestError(ctx extctx.ExtendedContext, w http.ResponseWriter, request *iso18626.Request, errorType iso18626.TypeErrorType, errorValue ErrorValue) {
	var resmsg = createRequestResponse(request, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	ctx.Logger().Warn("request confirmation error", "errorType", errorType, "errorValue", errorValue)
	writeResponse(ctx, resmsg, w)
}

func createPgText(value string) pgtype.Text {
	textValue := pgtype.Text{
		String: value,
		Valid:  true,
	}
	return textValue
}

func createRequestResponse(request *iso18626.Request, messageStatus iso18626.TypeMessageStatus, errorType *iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&request.Header, messageStatus)
	errorData := createErrorData(errorType, errorValue)
	resmsg.RequestConfirmation = &iso18626.RequestConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func createErrorData(errorType *iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ErrorData {
	if errorType != nil {
		var errorData = iso18626.ErrorData{
			ErrorType:  *errorType,
			ErrorValue: string(errorValue),
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

func handleIso18626RequestingAgencyMessage(ctx extctx.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo ill_db.IllRepo, eventBus events.EventBus) {
	var requestingRequestId = illMessage.RequestingAgencyMessage.Header.RequestingAgencyRequestId
	if requestingRequestId == "" {
		handleRequestingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdIsEmpty)
		return
	}

	eventData := events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: illMessage,
		},
	}

	var err error
	var illTrans ill_db.IllTransaction
	var action iso18626.TypeAction
	err = repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		illTrans, err = repo.GetIllTransactionByRequesterRequestIdForUpdate(ctx, createPgText(requestingRequestId))
		if err != nil {
			return err
		}
		action = validateAction(ctx, illMessage, w, eventData, eventBus, illTrans)
		if action == "" {
			return nil
		}
		illTrans.PrevRequesterAction = illTrans.LastRequesterAction
		illTrans.LastRequesterAction = createPgText(string(action))
		_, err = repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTrans))
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			ctx.Logger().Error(InternalFailedToLookupTx, "error", err)
			handleRequestingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdNotFound)
			return
		}
		ctx.Logger().Error(InternalFailedToSaveTx, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	if action == "" {
		return
	}
	ctx.Logger().Info("CROSSLINK-83: handleIso18626RequestingAgencyMessage SAVE", "action", action)

	eventId := createNoticeAndCheckDBError(ctx, w, eventBus, illTrans.ID, events.EventNameRequesterMsgReceived, eventData, events.EventStatusSuccess)
	if eventId == "" {
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	requestMapping[eventId] = RequestWait{
		w:  &w,
		wg: &wg,
	}
	wg.Wait()
}

func validateAction(ctx extctx.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, eventData events.EventData, eventBus events.EventBus, illTrans ill_db.IllTransaction) iso18626.TypeAction {
	action, ok := iso18626.ActionMap[string(illMessage.RequestingAgencyMessage.Action)]
	if !ok {
		resp := handleRequestingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnsupportedActionType, ErrorValue(fmt.Sprintf(string(InvalidAction), illMessage.RequestingAgencyMessage.Action)))
		eventData.OutgoingMessage = resp
		if createNoticeAndCheckDBError(ctx, w, eventBus, illTrans.ID, events.EventNameRequesterMsgReceived, eventData, events.EventStatusProblem) == "" {
			return ""
		}
		return ""
	}
	return action
}

func createRequestingAgencyResponse(illMessage *iso18626.ISO18626Message, messageStatus iso18626.TypeMessageStatus, errorType *iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&illMessage.RequestingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorType, errorValue)
	resmsg.RequestingAgencyMessageConfirmation = &iso18626.RequestingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
		Action:             &illMessage.RequestingAgencyMessage.Action,
	}
	return resmsg
}

func handleRequestingAgencyError(ctx extctx.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message, errorType iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	var resmsg = createRequestingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	ctx.Logger().Warn("requester message confirmation error", "errorType", errorType, "errorValue", errorValue)
	writeResponse(ctx, resmsg, w)
	return resmsg
}

func handleIso18626SupplyingAgencyMessage(ctx extctx.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo ill_db.IllRepo, eventBus events.EventBus) {
	var requestingRequestId = illMessage.SupplyingAgencyMessage.Header.RequestingAgencyRequestId
	if requestingRequestId == "" {
		handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdIsEmpty)
		return
	}

	var illTrans, err = repo.GetIllTransactionByRequesterRequestId(ctx, createPgText(requestingRequestId))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdNotFound)
			return
		}
		ctx.Logger().Error(InternalFailedToLookupTx, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}

	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusOK, nil, "")
	eventData := events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: illMessage,
			OutgoingMessage: resmsg,
		},
	}
	symbol := illMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text + ":" +
		illMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue
	status := validateStatusAndReasonForMessage(ctx, illMessage, w, eventData, eventBus, illTrans)
	if status == "" {
		return
	}
	err = updateLocatedSupplierStatus(ctx, repo, illTrans, symbol, status)
	if err != nil {
		ctx.Logger().Error("failed to update located supplier status", "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	if createNoticeAndCheckDBError(ctx, w, eventBus, illTrans.ID, events.EventNameSupplierMsgReceived, eventData, events.EventStatusSuccess) == "" {
		return
	}
	writeResponse(ctx, resmsg, w)
}

func validateStatusAndReasonForMessage(ctx extctx.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, eventData events.EventData, eventBus events.EventBus, illTrans ill_db.IllTransaction) iso18626.TypeStatus {
	status, ok := iso18626.StatusMap[string(illMessage.SupplyingAgencyMessage.StatusInfo.Status)]
	if !ok {
		resp := handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ErrorValue(fmt.Sprintf(string(InvalidStatus), illMessage.SupplyingAgencyMessage.StatusInfo.Status)))
		eventData.OutgoingMessage = resp
		if createNoticeAndCheckDBError(ctx, w, eventBus, illTrans.ID, events.EventNameSupplierMsgReceived, eventData, events.EventStatusProblem) == "" {
			return ""
		}
		return ""
	}
	_, ok = iso18626.ReasonForMassageMap[string(illMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)]
	if !ok {
		resp := handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnsupportedReasonForMessageType, ErrorValue(fmt.Sprintf(string(InvalidReason), illMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)))
		eventData.OutgoingMessage = resp
		if createNoticeAndCheckDBError(ctx, w, eventBus, illTrans.ID, events.EventNameSupplierMsgReceived, eventData, events.EventStatusProblem) == "" {
			return ""
		}
		return ""
	}
	return status
}

func updateLocatedSupplierStatus(ctx extctx.ExtendedContext, repo ill_db.IllRepo, illTrans ill_db.IllTransaction,
	symbol string, status iso18626.TypeStatus) error {
	return repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		peer, err := repo.GetPeerBySymbol(ctx, symbol)
		if err != nil {
			ctx.Logger().Error("failed to locate peer for symbol: "+symbol, "error", err)
			return err
		}
		locSup, err := repo.GetLocatedSupplierByIllTransactionAndSupplierForUpdate(ctx,
			ill_db.GetLocatedSupplierByIllTransactionAndSupplierForUpdateParams{
				IllTransactionID: illTrans.ID,
				SupplierID:       peer.ID,
			})
		if err != nil {
			ctx.Logger().Error("failed to get located supplier with peer id: "+peer.ID, "error", err)
			return err
		}
		locSup.PrevStatus = locSup.LastStatus
		locSup.LastStatus = createPgText(string(status))
		_, err = repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(locSup))
		if err != nil {
			ctx.Logger().Error("failed to update located supplier with id: "+locSup.ID, "error", err)
			return err
		}
		if status == iso18626.TypeStatusLoaned {
			updatePeerLoanCount(ctx, repo, peer)
			updatePeerBorrowCount(ctx, repo, illTrans)
		}
		return nil
	})
}

func updatePeerLoanCount(ctx extctx.ExtendedContext, repo ill_db.IllRepo, peer ill_db.Peer) {
	peer.LoansCount = peer.LoansCount + 1
	_, err := repo.SavePeer(ctx, ill_db.SavePeerParams(peer))
	if err != nil {
		ctx.Logger().Error("failed to update located supplier loans counter", "error", err)
	}
}
func updatePeerBorrowCount(ctx extctx.ExtendedContext, repo ill_db.IllRepo, illTrans ill_db.IllTransaction) {
	if illTrans.RequesterID.Valid {
		borrower, err := repo.GetPeerById(ctx, illTrans.RequesterID.String)
		if err != nil {
			ctx.Logger().Error("failed to read borrower", "error", err)
			return
		}
		borrower.BorrowsCount = borrower.BorrowsCount + 1
		_, err = repo.SavePeer(ctx, ill_db.SavePeerParams(borrower))
		if err != nil {
			ctx.Logger().Error("failed to update borrower borrows counter", "error", err)
		}
	}
}

func createSupplyingAgencyResponse(illMessage *iso18626.ISO18626Message, messageStatus iso18626.TypeMessageStatus, errorType *iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&illMessage.SupplyingAgencyMessage.Header, messageStatus)
	errorData := createErrorData(errorType, errorValue)
	resmsg.SupplyingAgencyMessageConfirmation = &iso18626.SupplyingAgencyMessageConfirmation{
		ConfirmationHeader: *header,
		ErrorData:          errorData,
	}
	return resmsg
}

func handleSupplyingAgencyError(ctx extctx.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message, errorType iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	ctx.Logger().Warn("supplier message confirmation error", "errorType", errorType, "errorValue", errorValue)
	writeResponse(ctx, resmsg, w)
	return resmsg
}

func createNoticeAndCheckDBError(ctx extctx.ExtendedContext, w http.ResponseWriter, eventBus events.EventBus, illTransId string, eventName events.EventName, eventData events.EventData, eventStatus events.EventStatus) string {
	id, err := eventBus.CreateNotice(illTransId, eventName, eventData, eventStatus)
	if err != nil {
		ctx.Logger().Error(InternalFailedToCreateNotice, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return ""
	}
	return id
}

func (h *Iso18626Handler) ConfirmRequesterMsg(ctx extctx.ExtendedContext, event events.Event) {
	h.eventBus.ProcessTask(ctx, event, h.handleConfirmRequesterMsgTask)
}

func (h *Iso18626Handler) handleConfirmRequesterMsgTask(ctx extctx.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
	status := events.EventStatusSuccess
	resData := events.EventResult{}
	responseEvent := h.eventBus.FindAncestor(ctx, &event, events.EventNameMessageSupplier)
	originalEvent := h.eventBus.FindAncestor(ctx, responseEvent, events.EventNameRequesterMsgReceived)
	if responseEvent != nil && originalEvent != nil {
		cResp, err := h.confirmSupplierResponse(ctx, originalEvent.ID, originalEvent.EventData.IncomingMessage, responseEvent.ResultData)
		resData.IncomingMessage = cResp
		if err != nil {
			status = events.EventStatusError
			resData.EventError = &events.EventError{
				Message: "failed tp confirm supplier message",
				Cause:   err.Error(),
			}
		}
	} else {
		status = events.EventStatusError
		resData.EventError = &events.EventError{
			Message: "missing ancestor events",
			Cause:   "message ancestor event missing",
		}
	}
	return status, &resData
}

func (c *Iso18626Handler) confirmSupplierResponse(ctx extctx.ExtendedContext, requestId string, illMessage *iso18626.ISO18626Message, result events.EventResult) (*iso18626.ISO18626Message, error) {
	wait, ok := requestMapping[requestId]
	if ok {
		delete(requestMapping, requestId)
		var errorMessage = ""
		var errorType *iso18626.TypeErrorType
		var messageStatus = iso18626.TypeMessageStatusOK
		if result.IncomingMessage != nil {
			if result.IncomingMessage.RequestingAgencyMessageConfirmation != nil {
				messageStatus = result.IncomingMessage.RequestingAgencyMessageConfirmation.ConfirmationHeader.MessageStatus
				if result.IncomingMessage.RequestingAgencyMessageConfirmation.ErrorData != nil {
					errorMessage = result.IncomingMessage.RequestingAgencyMessageConfirmation.ErrorData.ErrorValue
					errorType = &result.IncomingMessage.RequestingAgencyMessageConfirmation.ErrorData.ErrorType
				}
			}
		} else {
			// We don't have response, so it was http error or connection error
			if result.HttpFailure != nil {
				(*wait.w).WriteHeader(result.HttpFailure.StatusCode)
				if len(result.HttpFailure.Body) > 0 {
					(*wait.w).Write(result.HttpFailure.Body)
				}
				wait.wg.Done()
				return nil, result.HttpFailure
			}
			eType := iso18626.TypeErrorTypeBadlyFormedMessage
			errorMessage = string(CouldNotSendReqToPeer)
			errorType = &eType
			messageStatus = iso18626.TypeMessageStatusERROR
		}
		var resmsg = createRequestingAgencyResponse(illMessage, messageStatus, errorType, ErrorValue(errorMessage))
		writeResponse(ctx, resmsg, *wait.w)
		wait.wg.Done()
		return resmsg, nil
	} else {
		return nil, fmt.Errorf("cannot confirm request %s, not found", requestId)
	}
}

type RequestWait struct {
	w  *http.ResponseWriter
	wg *sync.WaitGroup
}
