package handler

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
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
	ReqIdAlreadyExists     ErrorValue = "requestingAgencyRequestId: request with a given ID already exists"
	ReqIdIsEmpty           ErrorValue = "requestingAgencyRequestId: cannot be empty"
	ReqIdNotFound          ErrorValue = "requestingAgencyRequestId: request with a given ID not found"
	SuppUniqueRecIdIsEmpty ErrorValue = "supplierUniqueRecordId: cannot be empty"
	ReqAgencyNotFound      ErrorValue = "requestingAgencyId: requesting agency not found"
)

const PublicFailedToProcessReqMsg = "failed to process request"
const InternalFailedToLookupTx = "failed to lookup ILL transaction"
const InternalFailedToSaveTx = "failed to save ILL transaction"
const InternalFailedToCreateNotice = "failed to create notice event"

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

func handleIso18626Request(ctx extctx.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo ill_db.IllRepo, eventBus events.EventBus, dirAdapter adapter.DirectoryLookupAdapter) {
	if illMessage.Request.Header.RequestingAgencyRequestId == "" {
		handleRequestError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdIsEmpty)
		return
	}

	if illMessage.Request.BibliographicInfo.SupplierUniqueRecordId == "" {
		handleRequestError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, SuppUniqueRecIdIsEmpty)
		return
	}

	requesterSymbol := createPgText(illMessage.Request.Header.RequestingAgencyId.AgencyIdType.Text + ":" + illMessage.Request.Header.RequestingAgencyId.AgencyIdValue)
	peers := repo.GetCachedPeersBySymbols(ctx, []string{requesterSymbol.String}, dirAdapter)
	if len(peers) != 1 {
		handleRequestError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqAgencyNotFound)
		return
	}
	supplierSymbol := createPgText(illMessage.Request.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + illMessage.Request.Header.SupplyingAgencyId.AgencyIdValue)
	requestAction := createPgText("Request")
	requesterRequestId := createPgText(illMessage.Request.Header.RequestingAgencyRequestId)
	supplierRequestId := createPgText(illMessage.Request.Header.SupplyingAgencyRequestId)

	illTransactionData := ill_db.IllTransactionData{
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
	_, err := repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{
		ID:                  id,
		Timestamp:           timestamp,
		RequesterSymbol:     requesterSymbol,
		RequesterID:         createPgText(peers[0].ID),
		LastRequesterAction: requestAction,
		SupplierSymbol:      supplierSymbol,
		RequesterRequestID:  requesterRequestId,
		SupplierRequestID:   supplierRequestId,
		IllTransactionData:  illTransactionData,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgerrcode.IsIntegrityConstraintViolation(pgErr.Code) {
			handleRequestError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdAlreadyExists)
			return
		} else {
			ctx.Logger().Error(InternalFailedToSaveTx, "error", err)
			http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
			return
		}
	}

	eventData := events.EventData{
		Timestamp:       getNow(),
		ISO18626Message: illMessage,
	}
	err = eventBus.CreateNotice(id, events.EventNameRequestReceived, eventData, events.EventStatusSuccess)
	if err != nil {
		ctx.Logger().Error(InternalFailedToCreateNotice, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}

	var resmsg = createRequestResponse(illMessage, iso18626.TypeMessageStatusOK, nil, "")
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

func handleRequestError(ctx extctx.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message, errorType iso18626.TypeErrorType, errorValue ErrorValue) {
	var resmsg = createRequestResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
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

func createRequestResponse(illMessage *iso18626.ISO18626Message, messageStatus iso18626.TypeMessageStatus, errorType *iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	var resmsg = &iso18626.ISO18626Message{}
	header := createConfirmationHeader(&illMessage.Request.Header, messageStatus)
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

	var illTrans, err = repo.GetIllTransactionByRequesterRequestId(ctx, createPgText(requestingRequestId))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			handleRequestingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdNotFound)
			return
		}
		ctx.Logger().Error(InternalFailedToLookupTx, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}

	illTrans.PrevRequesterAction = illTrans.LastRequesterAction
	illTrans.LastRequesterAction = createPgText(string(illMessage.RequestingAgencyMessage.Action))
	ctx.Logger().Info("AD: handle requesting agency message", "action", illTrans.LastRequesterAction)
	illTrans, err = repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTrans))
	if err != nil {
		ctx.Logger().Error(InternalFailedToSaveTx, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	if illTrans.LastRequesterAction.String != string(illMessage.RequestingAgencyMessage.Action) {
		ctx.Logger().Info("AD: returned different action", "action", illTrans.LastRequesterAction.String)
	}
	illTrans2, err := repo.GetIllTransactionById(ctx, illTrans.ID)
	if err != nil {
		ctx.Logger().Info("AD: error", "error", err.Error())
		ctx.Logger().Error(InternalFailedToLookupTx, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	if illTrans2.LastRequesterAction.String != illTrans.LastRequesterAction.String {
		ctx.Logger().Info("AD: mismatch", "action", illTrans.LastRequesterAction.String, "action2", illTrans2.LastRequesterAction.String)
	}
	eventData := events.EventData{
		Timestamp:       getNow(),
		ISO18626Message: illMessage,
	}
	err = eventBus.CreateNotice(illTrans.ID, events.EventNameRequesterMsgReceived, eventData, events.EventStatusSuccess)
	if err != nil {
		ctx.Logger().Error(InternalFailedToCreateNotice, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	//TODO we need to delay the confirmation until the supplier has responded
	var resmsg = createRequestingAgencyResponse(illMessage, iso18626.TypeMessageStatusOK, nil, "")
	writeResponse(ctx, resmsg, w)
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

func handleRequestingAgencyError(ctx extctx.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message, errorType iso18626.TypeErrorType, errorValue ErrorValue) {
	var resmsg = createRequestingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	ctx.Logger().Warn("requester message confirmation error", "errorType", errorType, "errorValue", errorValue)
	writeResponse(ctx, resmsg, w)
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

	eventData := events.EventData{
		Timestamp:       getNow(),
		ISO18626Message: illMessage,
	}
	err = eventBus.CreateNotice(illTrans.ID, events.EventNameSupplierMsgReceived, eventData, events.EventStatusSuccess)
	if err != nil {
		ctx.Logger().Error(InternalFailedToCreateNotice, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	symbol := illMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdType.Text + ":" + illMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue
	status := illMessage.SupplyingAgencyMessage.StatusInfo.Status
	updateLocatedSupplierStatus(ctx, repo, illTrans, symbol, status)
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusOK, nil, "")
	writeResponse(ctx, resmsg, w)
}

func updateLocatedSupplierStatus(ctx extctx.ExtendedContext, repo ill_db.IllRepo, illTrans ill_db.IllTransaction, symbol string, status iso18626.TypeStatus) {
	peer, err := repo.GetPeerBySymbol(ctx, symbol)
	if err != nil {
		ctx.Logger().Error("failed to locate peer for symbol: "+symbol, "error", err)
		return
	}
	locSup, err := repo.GetLocatedSupplierByIllTransactionAndSupplier(ctx, ill_db.GetLocatedSupplierByIllTransactionAndSupplierParams{
		IllTransactionID: illTrans.ID,
		SupplierID:       peer.ID,
	})
	if err != nil {
		ctx.Logger().Error("failed to get located supplier with peer id: "+peer.ID, "error", err)
		return
	}
	locSup.PrevStatus = locSup.LastStatus
	locSup.LastStatus = createPgText(string(status))
	_, err = repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(locSup))
	if err != nil {
		ctx.Logger().Error("failed to update located supplier with id: "+locSup.ID, "error", err)
		return
	}
	if status == iso18626.TypeStatusLoaned {
		updatePeerLoanCount(ctx, repo, peer)
		updatePeerBorrowCount(ctx, repo, illTrans)
	}
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

func handleSupplyingAgencyError(ctx extctx.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message, errorType iso18626.TypeErrorType, errorValue ErrorValue) {
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	ctx.Logger().Warn("supplier message confirmation error", "errorType", errorType, "errorValue", errorValue)
	writeResponse(ctx, resmsg, w)
}

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
