package handler

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/indexdata/crosslink/broker/shim"

	"github.com/indexdata/crosslink/broker/adapter"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var brokerSymbol = utils.GetEnv("BROKER_SYMBOL", "ISIL:BROKER")

const HANDLER_COMP = "iso18626_handler"
const ORIGINAL_INCOMING_MESSAGE = "originalIncomingMessage"

type ErrorValue string

const (
	ReqIdAlreadyExists        ErrorValue = "requestingAgencyRequestId: request with a given ID already exists"
	ReqIdIsEmpty              ErrorValue = "requestingAgencyRequestId: cannot be empty"
	ReqIdNotFound             ErrorValue = "requestingAgencyRequestId: request with a given ID not found"
	RetryNotPossible          ErrorValue = "requestType: Retry not possible"
	SupplierNotFoundOrInvalid ErrorValue = "supplyingAgencyId: supplying agency not found or invalid"
	UnsupportedRequestType    ErrorValue = "requestType: unsupported value"
	ReqAgencyNotFound         ErrorValue = "requestingAgencyId: requesting agency not found"
	CouldNotSendReqToPeer     ErrorValue = "Could not send request to peer"
	InvalidAction             ErrorValue = "%v is not a valid action"
	InvalidStatus             ErrorValue = "%v is not a valid status"
	InvalidReason             ErrorValue = "%v is not a valid reason"
)

const PublicFailedToProcessReqMsg = "failed to process request"
const InternalFailedToLookupTx = "failed to lookup ILL transaction"
const InternalFailedToSaveTx = "failed to save ILL transaction"
const InternalFailedToLookupSupplier = "failed to lookup supplier"
const InternalFailedToCreateNotice = "failed to create notice event"
const InternalFailedToConfirmRequesterMessage = "failed to confirm requester message"
const InternalFailedToConfirmSupplierMessage = "failed to confirm supplier message"

var ErrRetryNotPossible = errors.New(string(RetryNotPossible))

var waitingReqs = map[string]RequestWait{}

type Iso18626HandlerInterface interface {
	HandleRequest(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter)
	HandleRequestingAgencyMessage(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter)
}

type Iso18626Handler struct {
	eventBus   events.EventBus
	eventRepo  events.EventRepo
	illRepo    ill_db.IllRepo
	dirAdapter adapter.DirectoryLookupAdapter
}

func CreateIso18626Handler(eventBus events.EventBus, eventRepo events.EventRepo, illRepo ill_db.IllRepo, dirAdapter adapter.DirectoryLookupAdapter) Iso18626Handler {
	return Iso18626Handler{
		eventBus:   eventBus,
		eventRepo:  eventRepo,
		illRepo:    illRepo,
		dirAdapter: dirAdapter,
	}
}

func Iso18626PostHandler(repo ill_db.IllRepo, eventBus events.EventBus, dirAdapter adapter.DirectoryLookupAdapter, maxMsgSize int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{RequestId: uuid.NewString(), Component: HANDLER_COMP})
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
		r.Body = http.MaxBytesReader(w, r.Body, int64(maxMsgSize))
		byteReq, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				ctx.Logger().Error("request body too large", "error", err)
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
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
			handleRequest(ctx, &illMessage, w, repo, eventBus, dirAdapter)
		} else if illMessage.RequestingAgencyMessage != nil {
			handleRequestingAgencyMessage(ctx, &illMessage, w, repo, eventBus)
		} else if illMessage.SupplyingAgencyMessage != nil {
			handleSupplyingAgencyMessage(ctx, &illMessage, w, repo, eventBus)
		} else {
			ctx.Logger().Error("invalid ISO18626 message", "error", err, "body", string(byteReq))
			http.Error(w, "invalid ISO18626 message", http.StatusBadRequest)
			return
		}
	}
}

func handleNewRequest(ctx common.ExtendedContext, request *iso18626.Request, repo ill_db.IllRepo, requesterSymbol pgtype.Text, peers []ill_db.Peer) (string, error) {
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

func handleRetryRequest(ctx common.ExtendedContext, request *iso18626.Request, repo ill_db.IllRepo) (string, error) {
	// ServiceInfo already nil checked in handleIso18626Request
	prevReqId := createPgText(request.ServiceInfo.RequestingAgencyPreviousRequestId)

	var id string
	err := repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		illTrans, err := repo.GetIllTransactionByRequesterRequestIdForUpdate(ctx, prevReqId)
		if err != nil {
			return ErrRetryNotPossible
		}
		selSup, err := repo.GetSelectedSupplierForIllTransaction(ctx, illTrans.ID)
		if err != nil {
			return ErrRetryNotPossible
		}
		if selSup.LastStatus.String != string(iso18626.TypeStatusRetryPossible) {
			return ErrRetryNotPossible
		}
		requesterRequestId := createPgText(request.Header.RequestingAgencyRequestId)
		illTrans.RequesterRequestID = requesterRequestId
		illTrans.PrevRequesterRequestID = prevReqId
		id = illTrans.ID

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

func (h *Iso18626Handler) HandleRequest(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	handleRequest(ctx, illMessage, w, h.illRepo, h.eventBus, h.dirAdapter)
}

func handleRequest(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo ill_db.IllRepo, eventBus events.EventBus, dirAdapter adapter.DirectoryLookupAdapter) {
	request := illMessage.Request
	if request.Header.RequestingAgencyRequestId == "" {
		handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdIsEmpty)
		return
	}

	requesterSymbol := createPgText(request.Header.RequestingAgencyId.AgencyIdType.Text + ":" + request.Header.RequestingAgencyId.AgencyIdValue)
	peers, _, _ := repo.GetCachedPeersBySymbols(ctx, []string{requesterSymbol.String}, dirAdapter)
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
		} else if errors.Is(err, ErrRetryNotPossible) {
			handleRequestError(ctx, w, request, iso18626.TypeErrorTypeUnrecognisedDataValue, RetryNotPossible)
		} else {
			ctx.Logger().Error(InternalFailedToSaveTx, "error", err)
			http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		}
		return
	}
	afterShim := shim.GetShim(peers[0].Vendor).ApplyToIncomingRequest(illMessage, &peers[0], nil)
	var resmsg = createRequestResponse(request, iso18626.TypeMessageStatusOK, nil, "")
	eventData := events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: afterShim,
			OutgoingMessage: resmsg,
		},
		CustomData: map[string]any{
			ORIGINAL_INCOMING_MESSAGE: illMessage,
		},
	}
	if _, err = createNotice(ctx, eventBus, id, event, eventData, events.EventStatusSuccess); err != nil {
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	writeResponse(ctx, resmsg, w)
}

func writeResponse(ctx common.ExtendedContext, resmsg *iso18626.ISO18626Message, w http.ResponseWriter) {
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

func handleRequestError(ctx common.ExtendedContext, w http.ResponseWriter, request *iso18626.Request, errorType iso18626.TypeErrorType, errorValue ErrorValue) {
	ctx.Logger().Warn("request confirmation error", "errorType", errorType, "errorValue", errorValue,
		"requesterSymbol", request.Header.RequestingAgencyId.AgencyIdValue,
		"supplierSymbol", request.Header.SupplyingAgencyId.AgencyIdValue,
		"requesterRequestId", request.Header.RequestingAgencyRequestId)
	var resmsg = createRequestResponse(request, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
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

func (h *Iso18626Handler) HandleRequestingAgencyMessage(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter) {
	handleRequestingAgencyMessage(ctx, illMessage, w, h.illRepo, h.eventBus)
}

func handleRequestingAgencyMessage(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo ill_db.IllRepo, eventBus events.EventBus) {
	var requestingRequestId = illMessage.RequestingAgencyMessage.Header.RequestingAgencyRequestId
	if requestingRequestId == "" {
		handleRequestingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqIdIsEmpty)
		return
	}
	symbol := getSupplierSymbol(&illMessage.RequestingAgencyMessage.Header)
	if len(symbol) == 0 {
		handleRequestingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, SupplierNotFoundOrInvalid)
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
	errorValue := ReqIdNotFound
	err = repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		illTrans, err = repo.GetIllTransactionByRequesterRequestIdForUpdate(ctx, createPgText(requestingRequestId))
		if err != nil {
			errorValue = ReqIdNotFound
			return err
		}
		supp, supErr := repo.GetSelectedSupplierForIllTransaction(ctx, illTrans.ID)
		if symbol != brokerSymbol {
			if supErr != nil {
				errorValue = SupplierNotFoundOrInvalid
				return supErr
			}
			if supp.SupplierSymbol != symbol {
				errorValue = SupplierNotFoundOrInvalid
				return pgx.ErrNoRows
			}
		}
		if illTrans.RequesterID.Valid {
			eValue, errShim := applyRequesterShim(ctx, repo, illTrans.RequesterID.String, illMessage, &eventData, &supp)
			if errShim != nil {
				errorValue = eValue
				return errShim
			}
		}
		action, err = validateAction(ctx, w, eventData, eventBus, illTrans)
		if err != nil {
			return err
		}
		illTrans.PrevRequesterAction = illTrans.LastRequesterAction
		illTrans.LastRequesterAction = createPgText(string(action))
		_, err = repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams(illTrans))
		return err
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			handleRequestingAgencyError(ctx, w, eventData.IncomingMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, errorValue)
			return
		}
		ctx.Logger().Error(InternalFailedToSaveTx, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	if action == "" {
		return
	}

	eventId, err := createNotice(ctx, eventBus, illTrans.ID, events.EventNameRequesterMsgReceived, eventData, events.EventStatusSuccess)
	if err != nil {
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	waitingReqs[eventId] = RequestWait{
		w:  &w,
		wg: &wg,
	}
	wg.Wait()
}

func applyRequesterShim(ctx common.ExtendedContext, repo ill_db.IllRepo, reqId string, illMessage *iso18626.ISO18626Message, eventData *events.EventData, supplier *ill_db.LocatedSupplier) (ErrorValue, error) {
	requester, err := repo.GetPeerById(ctx, reqId)
	if err != nil {
		return ReqAgencyNotFound, err
	}
	afterShim := shim.GetShim(requester.Vendor).ApplyToIncomingRequest(illMessage, &requester, supplier)
	eventData.IncomingMessage = afterShim
	eventData.CustomData = map[string]any{
		ORIGINAL_INCOMING_MESSAGE: illMessage,
	}
	return "", nil
}

func getSupplierSymbol(header *iso18626.Header) string {
	if len(header.SupplyingAgencyId.AgencyIdType.Text) == 0 || len(header.SupplyingAgencyId.AgencyIdValue) == 0 {
		return ""
	}
	return header.SupplyingAgencyId.AgencyIdType.Text + ":" +
		header.SupplyingAgencyId.AgencyIdValue
}

func validateAction(ctx common.ExtendedContext, w http.ResponseWriter, eventData events.EventData, eventBus events.EventBus, illTrans ill_db.IllTransaction) (iso18626.TypeAction, error) {
	action, ok := iso18626.ActionMap[string(eventData.IncomingMessage.RequestingAgencyMessage.Action)]
	if !ok {
		err := fmt.Errorf(string(InvalidAction), eventData.IncomingMessage.RequestingAgencyMessage.Action)
		resp := handleRequestingAgencyError(ctx, w, eventData.IncomingMessage, iso18626.TypeErrorTypeUnsupportedActionType, ErrorValue(err.Error()))
		eventData.OutgoingMessage = resp
		_, _ = createNotice(ctx, eventBus, illTrans.ID, events.EventNameRequesterMsgReceived, eventData, events.EventStatusProblem)
		return "", err
	}
	return action, nil
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

func handleRequestingAgencyError(ctx common.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message, errorType iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	ctx.Logger().Warn("requester message confirmation error", "errorType", errorType, "errorValue", errorValue,
		"requesterSymbol", illMessage.RequestingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue,
		"supplierSymbol", illMessage.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue,
		"requesterRequestId", illMessage.RequestingAgencyMessage.Header.RequestingAgencyRequestId)
	var resmsg = createRequestingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	//TODO create error notice when possible
	writeResponse(ctx, resmsg, w)
	return resmsg
}

func handleSupplyingAgencyMessage(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, repo ill_db.IllRepo, eventBus events.EventBus) {
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
	symbol := getSupplierSymbol(&illMessage.SupplyingAgencyMessage.Header)
	if len(symbol) == 0 {
		handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, SupplierNotFoundOrInvalid)
		return
	}
	requester, err := repo.GetPeerById(ctx, illTrans.RequesterID.String)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ReqAgencyNotFound)
			return
		}
		ctx.Logger().Error(InternalFailedToLookupTx, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	supplier, err := repo.GetSelectedSupplierForIllTransaction(ctx, illTrans.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, pgx.ErrTooManyRows) {
		ctx.Logger().Error(InternalFailedToLookupSupplier, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, pgx.ErrTooManyRows) || supplier.SupplierSymbol != symbol {
		// we allow notification from skipped suppliers
		if illMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage == iso18626.TypeReasonForMessageNotification {
			supplier, err = repo.GetLocatedSupplierByIllTransactionAndSymbol(ctx, illTrans.ID, symbol)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, pgx.ErrTooManyRows) {
				ctx.Logger().Error(InternalFailedToLookupSupplier, "error", err)
				http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
				return
			}
			if supplier.SupplierStatus != ill_db.SupplierStateSkippedPg {
				handleSupplyingAgencyErrorWithNotice(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, SupplierNotFoundOrInvalid,
					eventBus, illTrans.ID)
				return
			}
		} else {
			handleSupplyingAgencyErrorWithNotice(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, SupplierNotFoundOrInvalid,
				eventBus, illTrans.ID)
			return
		}
	}
	if supplier.SupplierSymbol != symbol { //ensure we found the correct supplier
		handleSupplyingAgencyErrorWithNotice(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, SupplierNotFoundOrInvalid,
			eventBus, illTrans.ID)
		return
	}
	supplierPeer, err := repo.GetPeerById(ctx, supplier.SupplierID)
	if err != nil {
		ctx.Logger().Error(InternalFailedToLookupSupplier, "error", err)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	afterShim := shim.GetShim(supplierPeer.Vendor).ApplyToIncomingRequest(illMessage, &requester, &supplier)
	var resmsg = createSupplyingAgencyResponse(afterShim, iso18626.TypeMessageStatusOK, nil, "")
	eventData := events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: afterShim,
			OutgoingMessage: resmsg,
		},
		CustomData: map[string]any{
			ORIGINAL_INCOMING_MESSAGE: illMessage,
		},
	}

	supReqId := afterShim.SupplyingAgencyMessage.Header.SupplyingAgencyRequestId
	status, reason, err := validateStatusAndReasonForMessage(ctx, afterShim, w, eventData, eventBus, illTrans)
	if err != nil {
		return
	}
	err = updateLocatedSupplier(ctx, repo, illTrans, status, reason, supReqId, supplierPeer.ID, supplier.ID)
	if err != nil {
		ctx.Logger().Error("failed to update located supplier status to: "+string(status), "error", err, "transactionId", illTrans.ID)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}

	eventId, err := createNotice(ctx, eventBus, illTrans.ID, events.EventNameSupplierMsgReceived, eventData, events.EventStatusSuccess)
	if err != nil {
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
		return
	}
	var wg sync.WaitGroup
	wg.Add(1)
	waitingReqs[eventId] = RequestWait{
		w:  &w,
		wg: &wg,
	}
	wg.Wait()
}

func validateStatusAndReasonForMessage(ctx common.ExtendedContext, illMessage *iso18626.ISO18626Message, w http.ResponseWriter, eventData events.EventData, eventBus events.EventBus, illTrans ill_db.IllTransaction) (iso18626.TypeStatus, iso18626.TypeReasonForMessage, error) {
	status := illMessage.SupplyingAgencyMessage.StatusInfo.Status
	if len(status) > 0 {
		var ok bool
		status, ok = iso18626.StatusMap[string(status)]
		if !ok {
			err := fmt.Errorf(string(InvalidStatus), illMessage.SupplyingAgencyMessage.StatusInfo.Status)
			resp := handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnrecognisedDataValue, ErrorValue(err.Error()))
			eventData.OutgoingMessage = resp
			_, _ = createNotice(ctx, eventBus, illTrans.ID, events.EventNameSupplierMsgReceived, eventData, events.EventStatusProblem)
			return "", "", err
		}
	} else {
		// suppliers like Alma/Rapido, may send an empty status to indicate no status change
		status = ""
	}
	reason, ok := iso18626.ReasonForMassageMap[string(illMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)]
	if !ok {
		err := fmt.Errorf(string(InvalidReason), illMessage.SupplyingAgencyMessage.MessageInfo.ReasonForMessage)
		resp := handleSupplyingAgencyError(ctx, w, illMessage, iso18626.TypeErrorTypeUnsupportedReasonForMessageType, ErrorValue(err.Error()))
		eventData.OutgoingMessage = resp
		_, _ = createNotice(ctx, eventBus, illTrans.ID, events.EventNameSupplierMsgReceived, eventData, events.EventStatusProblem)
		return "", "", err
	}
	return status, reason, nil
}

func updateLocatedSupplier(ctx common.ExtendedContext, repo ill_db.IllRepo, illTrans ill_db.IllTransaction,
	status iso18626.TypeStatus, reason iso18626.TypeReasonForMessage, supReqId string, supPeerId string, supId string) error {
	return repo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		locSup, err := repo.GetLocatedSupplierByIdForUpdate(ctx, supId)
		if err != nil {
			ctx.Logger().Error("failed to read located supplier with id: "+supId, "error", err, "transactionId", illTrans.ID)
			return err
		}
		if iso18626.IsTransitionValid(iso18626.TypeStatus(locSup.LastStatus.String), status) {
			// transition is valid but only update if it's not the same status to keep the history clean
			if locSup.LastStatus.String != string(status) {
				locSup.PrevStatus = locSup.LastStatus
				locSup.LastStatus = createPgText(string(status))
			}
		} else {
			level := slog.LevelWarn
			if reason == iso18626.TypeReasonForMessageNotification {
				// notifications usually have wrong status transition so keep the log noise low
				level = slog.LevelInfo
			}
			ctx.Logger().Log(ctx, level, "ignoring invalid status transition", "from", locSup.LastStatus.String, "to", status, "reason", reason, "transactionId", illTrans.ID)
		}
		locSup.PrevReason = locSup.LastReason
		locSup.LastReason = createPgText(string(reason))
		if supReqId != "" {
			locSup.SupplierRequestID = createPgText(supReqId)
		}
		_, err = repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(locSup))
		if err != nil {
			ctx.Logger().Error("failed to update located supplier with id: "+locSup.ID, "error", err, "transactionId", illTrans.ID)
			return err
		}
		if status == iso18626.TypeStatusLoaned {
			updatePeerLoanCount(ctx, repo, supPeerId, illTrans.ID)
			updatePeerBorrowCount(ctx, repo, illTrans)
		}
		return nil
	})
}

func updatePeerLoanCount(ctx common.ExtendedContext, repo ill_db.IllRepo, supPeerId string, illTransId string) {
	peer, err := repo.GetPeerById(ctx, supPeerId)
	if err != nil {
		ctx.Logger().Error("failed to locate supplier peer for id: "+supPeerId, "error", err, "transactionId", illTransId)
	}
	peer.LoansCount = peer.LoansCount + 1
	_, err = repo.SavePeer(ctx, ill_db.SavePeerParams(peer))
	if err != nil {
		ctx.Logger().Error("failed to update located supplier loans counter", "error", err, "transactionId", illTransId)
	}
}
func updatePeerBorrowCount(ctx common.ExtendedContext, repo ill_db.IllRepo, illTrans ill_db.IllTransaction) {
	if illTrans.RequesterID.Valid {
		borrower, err := repo.GetPeerById(ctx, illTrans.RequesterID.String)
		if err != nil {
			ctx.Logger().Error("failed to read requester peer", "error", err, "transactionId", illTrans.ID)
			return
		}
		borrower.BorrowsCount = borrower.BorrowsCount + 1
		_, err = repo.SavePeer(ctx, ill_db.SavePeerParams(borrower))
		if err != nil {
			ctx.Logger().Error("failed to update requester borrows counter", "error", err, "transactionId", illTrans.ID)
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

func handleSupplyingAgencyError(ctx common.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message, errorType iso18626.TypeErrorType, errorValue ErrorValue) *iso18626.ISO18626Message {
	ctx.Logger().Warn("supplier message confirmation error", "errorType", errorType, "errorValue", errorValue,
		"requesterSymbol", illMessage.SupplyingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue,
		"supplierSymbol", illMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue,
		"requesterRequestId", illMessage.SupplyingAgencyMessage.Header.RequestingAgencyRequestId)
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	writeResponse(ctx, resmsg, w)
	return resmsg
}

func handleSupplyingAgencyErrorWithNotice(ctx common.ExtendedContext, w http.ResponseWriter, illMessage *iso18626.ISO18626Message,
	errorType iso18626.TypeErrorType, errorValue ErrorValue,
	eventBus events.EventBus, illTransId string) {
	ctx.Logger().Warn("supplier message confirmation error", "errorType", errorType, "errorValue", errorValue, "transactionId", illTransId,
		"requesterSymbol", illMessage.SupplyingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue,
		"supplierSymbol", illMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue,
		"requesterRequestId", illMessage.SupplyingAgencyMessage.Header.RequestingAgencyRequestId)
	var resmsg = createSupplyingAgencyResponse(illMessage, iso18626.TypeMessageStatusERROR, &errorType, errorValue)
	eventData := events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: illMessage,
			OutgoingMessage: resmsg,
			Problem: &events.Problem{
				Kind:    "supplier-message-problem",
				Details: string(errorValue),
			},
		},
	}
	_, err := eventBus.CreateNotice(illTransId, events.EventNameSupplierMsgReceived, eventData, events.EventStatusProblem, events.EventDomainIllTransaction)
	if err != nil {
		ctx.Logger().Error(InternalFailedToCreateNotice, "error", err, "transactionId", illTransId)
		http.Error(w, PublicFailedToProcessReqMsg, http.StatusInternalServerError)
	} else {
		writeResponse(ctx, resmsg, w)
	}
}

func createNotice(ctx common.ExtendedContext, eventBus events.EventBus, illTransId string, eventName events.EventName, eventData events.EventData, eventStatus events.EventStatus) (string, error) {
	id, err := eventBus.CreateNotice(illTransId, eventName, eventData, eventStatus, events.EventDomainIllTransaction)
	if err != nil {
		ctx.Logger().Error(InternalFailedToCreateNotice, "error", err, "transactionId", illTransId)
		return "", err
	}
	return id, nil
}

func (h *Iso18626Handler) ConfirmRequesterMsg(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(HANDLER_COMP))
	// called for all event bus instances.
	suppResponseEvent := h.eventBus.FindAncestor(&event, events.EventNameMessageSupplier)
	if suppResponseEvent == nil {
		// all instances will try to process the event on lookup failure
		_, _ = h.eventBus.ProcessTask(ctx, event, func(ec common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
			return handleConfirmReqMsgMissingAncestor(ec, e, fmt.Errorf("ancestor event %s missing", events.EventNameMessageSupplier))
		})
		return
	}
	reqRequestEvent := h.eventBus.FindAncestor(suppResponseEvent, events.EventNameRequesterMsgReceived)
	if reqRequestEvent == nil {
		// all instances will try to process the event on lookup failure
		_, _ = h.eventBus.ProcessTask(ctx, event, func(ec common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
			return handleConfirmReqMsgMissingAncestor(ec, e, fmt.Errorf("ancestor event %s missing", events.EventNameRequesterMsgReceived))
		})
		return
	}
	if _, ok := waitingReqs[reqRequestEvent.ID]; !ok {
		return // instance doesn't have the paused request
	}
	// instance has the event, process it
	_, _ = h.eventBus.ProcessTask(ctx, event, func(ec common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
		return h.handleConfirmRequesterMsgTask(ec, e, reqRequestEvent.ID, reqRequestEvent.EventData.IncomingMessage, suppResponseEvent.ResultData)
	})
}

func handleConfirmReqMsgMissingAncestor(ctx common.ExtendedContext, event events.Event, cause error) (events.EventStatus, *events.EventResult) {
	ctx.Logger().Warn(InternalFailedToConfirmRequesterMessage, "error", cause, "eventId", event.ID, "transactionId", event.IllTransactionID)
	status := events.EventStatusError
	resData := events.EventResult{}
	resData.EventError = &events.EventError{
		Message: InternalFailedToConfirmRequesterMessage,
		Cause:   cause.Error(),
	}
	return status, &resData
}

func (h *Iso18626Handler) ConfirmSupplierMsg(ctx common.ExtendedContext, event events.Event) {
	ctx = ctx.WithArgs(ctx.LoggerArgs().WithComponent(HANDLER_COMP))
	parent := h.eventBus.FindAncestor(&event, events.EventNameMessageRequester)
	if parent == nil {
		// message was not forwarded
		parent = &event
		parent.ResultData = events.EventResult{}
		parent.ResultData.IncomingMessage = suppMsgOkConfirmation()
	}
	supRequestEvent := h.eventBus.FindAncestor(parent, events.EventNameSupplierMsgReceived)
	if supRequestEvent == nil {
		// all instances will try to process the event on lookup failure
		_, _ = h.eventBus.ProcessTask(ctx, event, func(ec common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
			return handleConfirmSuppMsgMissingAncestor(ec, e, fmt.Errorf("ancestor event %s missing", events.EventNameSupplierMsgReceived))
		})
		return
	}
	if _, ok := waitingReqs[supRequestEvent.ID]; !ok {
		return // instance doesn't have the paused request
	}
	// instance has the event, process it
	_, _ = h.eventBus.ProcessTask(ctx, event, func(ec common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
		return h.handleConfirmSupplierMsgTask(ec, e, supRequestEvent.ID, supRequestEvent.EventData.IncomingMessage, parent.ResultData)
	})
}

func suppMsgOkConfirmation() *iso18626.ISO18626Message {
	return &iso18626.ISO18626Message{
		SupplyingAgencyMessageConfirmation: &iso18626.SupplyingAgencyMessageConfirmation{
			ConfirmationHeader: iso18626.ConfirmationHeader{
				MessageStatus: iso18626.TypeMessageStatusOK,
			},
		},
	}
}

func handleConfirmSuppMsgMissingAncestor(ctx common.ExtendedContext, event events.Event, cause error) (events.EventStatus, *events.EventResult) {
	ctx.Logger().Warn(InternalFailedToConfirmSupplierMessage, "error", cause, "eventId", event.ID, "transactionId", event.IllTransactionID)

	status := events.EventStatusError
	resData := events.EventResult{}
	resData.EventError = &events.EventError{
		Message: InternalFailedToConfirmSupplierMessage,
		Cause:   cause.Error(),
	}
	return status, &resData
}

func (h *Iso18626Handler) handleConfirmSupplierMsgTask(ctx common.ExtendedContext, event events.Event,
	waitRequestId string, supplierIllMsg *iso18626.ISO18626Message, supplierResult events.EventResult) (events.EventStatus, *events.EventResult) {
	status := events.EventStatusSuccess
	resData := events.EventResult{}
	cResp, err := h.confirmRequesterResponse(ctx, event.IllTransactionID, waitRequestId, supplierIllMsg, supplierResult)
	resData.IncomingMessage = cResp
	if err != nil {
		ctx.Logger().Error(InternalFailedToConfirmSupplierMessage, "error", err, "eventId", event.ID, "transactionId", event.IllTransactionID)
		status = events.EventStatusError
		resData.EventError = &events.EventError{
			Message: InternalFailedToConfirmSupplierMessage,
			Cause:   err.Error(),
		}
	}
	return status, &resData
}

func (h *Iso18626Handler) handleConfirmRequesterMsgTask(ctx common.ExtendedContext, event events.Event,
	waitRequestId string, requesterIllMsg *iso18626.ISO18626Message, supplierResult events.EventResult) (events.EventStatus, *events.EventResult) {
	status := events.EventStatusSuccess
	resData := events.EventResult{}
	cResp, err := h.confirmSupplierResponse(ctx, event.IllTransactionID, waitRequestId, requesterIllMsg, supplierResult)
	resData.IncomingMessage = cResp
	if err != nil {
		ctx.Logger().Error(InternalFailedToConfirmRequesterMessage, "error", err, "eventId", event.ID, "transactionId", event.IllTransactionID)
		status = events.EventStatusError
		resData.EventError = &events.EventError{
			Message: InternalFailedToConfirmRequesterMessage,
			Cause:   err.Error(),
		}
	}
	return status, &resData
}

func (c *Iso18626Handler) confirmSupplierResponse(ctx common.ExtendedContext, illTransId string, waitRequestId string, requesterIllMsg *iso18626.ISO18626Message,
	supplierResult events.EventResult) (*iso18626.ISO18626Message, error) {
	wait, ok := waitingReqs[waitRequestId]
	if !ok {
		return nil, fmt.Errorf("waiting request '%s' not found", waitRequestId)
	}
	delete(waitingReqs, waitRequestId)
	var errorMessage = ""
	var errorType *iso18626.TypeErrorType
	var messageStatus = iso18626.TypeMessageStatusOK
	if supplierResult.IncomingMessage != nil {
		if supplierResult.IncomingMessage.RequestingAgencyMessageConfirmation != nil {
			confirmMsg := supplierResult.IncomingMessage.RequestingAgencyMessageConfirmation
			messageStatus = confirmMsg.ConfirmationHeader.MessageStatus
			if messageStatus != iso18626.TypeMessageStatusOK {
				if confirmMsg.ErrorData != nil {
					errorMessage = confirmMsg.ErrorData.ErrorValue
					errorType = &confirmMsg.ErrorData.ErrorType
				}
				var eType string
				if errorType != nil {
					eType = string(*errorType)
				}
				var requesterSymbol string
				if confirmMsg.ConfirmationHeader.RequestingAgencyId != nil {
					requesterSymbol = string(confirmMsg.ConfirmationHeader.RequestingAgencyId.AgencyIdValue)
				}
				var supplierSymbol string
				if confirmMsg.ConfirmationHeader.SupplyingAgencyId != nil {
					supplierSymbol = string(confirmMsg.ConfirmationHeader.SupplyingAgencyId.AgencyIdValue)
				}
				ctx.Logger().Warn("forwarding requester message confirmation error", "errorType", eType, "errorValue", errorMessage, "transactionId", illTransId,
					"requesterSymbol", requesterSymbol,
					"supplierSymbol", supplierSymbol,
					"requesterRequestId", confirmMsg.ConfirmationHeader.RequestingAgencyRequestId)
			}
		}
	} else if doNotSend, foundOk := supplierResult.CustomData[common.DO_NOT_SEND].(bool); foundOk && doNotSend {
		// message was not forwarded so reply with ok
		messageStatus = iso18626.TypeMessageStatusOK
	} else {
		// We don't have response, so it was http error or connection error
		if supplierResult.HttpFailure != nil {
			(*wait.w).WriteHeader(supplierResult.HttpFailure.StatusCode)
			if len(supplierResult.HttpFailure.Body) > 0 {
				(*wait.w).Write(supplierResult.HttpFailure.Body)
			}
			wait.wg.Done()
			ctx.Logger().Warn("forwarding HTTP error response from supplier to requester", "transactionId", illTransId, "error", supplierResult.HttpFailure)
			return nil, supplierResult.HttpFailure
		}
		eType := iso18626.TypeErrorTypeBadlyFormedMessage
		errorMessage = string(CouldNotSendReqToPeer)
		errorType = &eType
		messageStatus = iso18626.TypeMessageStatusERROR
		ctx.Logger().Warn("requester message confirmation error, error while sending message to supplier", "transactionId", illTransId,
			"errorType", eType, "errorValue", errorMessage)
	}
	var resmsg = createRequestingAgencyResponse(requesterIllMsg, messageStatus, errorType, ErrorValue(errorMessage))
	writeResponse(ctx, resmsg, *wait.w)
	wait.wg.Done()
	return resmsg, nil
}

func (c *Iso18626Handler) confirmRequesterResponse(ctx common.ExtendedContext, illTransId string, waitRequestId string, supplierIllMsg *iso18626.ISO18626Message,
	requesterResult events.EventResult) (*iso18626.ISO18626Message, error) {
	wait, ok := waitingReqs[waitRequestId]
	if !ok {
		return nil, fmt.Errorf("waiting request '%s' not found", waitRequestId)
	}
	delete(waitingReqs, waitRequestId)
	var errorMessage = ""
	var errorType *iso18626.TypeErrorType
	var messageStatus iso18626.TypeMessageStatus
	if requesterResult.IncomingMessage != nil && requesterResult.IncomingMessage.SupplyingAgencyMessageConfirmation != nil {
		confirmMsg := requesterResult.IncomingMessage.SupplyingAgencyMessageConfirmation
		messageStatus = confirmMsg.ConfirmationHeader.MessageStatus
		if messageStatus != iso18626.TypeMessageStatusOK {
			if confirmMsg.ErrorData != nil {
				errorMessage = confirmMsg.ErrorData.ErrorValue
				errorType = &confirmMsg.ErrorData.ErrorType
			}
			var eType string
			if errorType != nil {
				eType = string(*errorType)
			}
			var requesterSymbol string
			if confirmMsg.ConfirmationHeader.RequestingAgencyId != nil {
				requesterSymbol = string(confirmMsg.ConfirmationHeader.RequestingAgencyId.AgencyIdValue)
			}
			var supplierSymbol string
			if confirmMsg.ConfirmationHeader.SupplyingAgencyId != nil {
				supplierSymbol = string(confirmMsg.ConfirmationHeader.SupplyingAgencyId.AgencyIdValue)
			}
			ctx.Logger().Warn("forwarding supplying message confirmation error", "errorType", eType, "errorValue", errorMessage, "transactionId", illTransId,
				"requesterSymbol", requesterSymbol,
				"supplierSymbol", supplierSymbol,
				"requesterRequestId", confirmMsg.ConfirmationHeader.RequestingAgencyRequestId)
		}
	} else if requesterResult.HttpFailure != nil {
		// We don't have response, so it was http error or connection error
		(*wait.w).WriteHeader(requesterResult.HttpFailure.StatusCode)
		if len(requesterResult.HttpFailure.Body) > 0 {
			(*wait.w).Write(requesterResult.HttpFailure.Body)
		}
		wait.wg.Done()
		ctx.Logger().Warn("forwarding HTTP error response from requester to supplier", "transactionId", illTransId, "error", requesterResult.HttpFailure)
		return nil, requesterResult.HttpFailure
	} else if doNotSend, foundOk := requesterResult.CustomData[common.DO_NOT_SEND].(bool); foundOk && doNotSend {
		// message was not forwarded so reply with ok
		messageStatus = iso18626.TypeMessageStatusOK
	} else {
		eType := iso18626.TypeErrorTypeBadlyFormedMessage
		errorMessage = string(CouldNotSendReqToPeer)
		errorType = &eType
		messageStatus = iso18626.TypeMessageStatusERROR
		ctx.Logger().Warn("supplier message confirmation error, error while sending message to requester", "transactionId", illTransId,
			"errorType", eType, "errorValue", errorMessage)
	}
	var resmsg = createSupplyingAgencyResponse(supplierIllMsg, messageStatus, errorType, ErrorValue(errorMessage))
	writeResponse(ctx, resmsg, *wait.w)
	wait.wg.Done()
	return resmsg, nil
}

type RequestWait struct {
	w  *http.ResponseWriter
	wg *sync.WaitGroup
}
