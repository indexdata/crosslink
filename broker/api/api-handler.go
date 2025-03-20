package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/indexdata/go-utils/utils"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type ApiHandler struct {
	eventRepo events.EventRepo
	illRepo   ill_db.IllRepo
}

func NewApiHandler(eventRepo events.EventRepo, illRepo ill_db.IllRepo) ApiHandler {
	return ApiHandler{
		eventRepo: eventRepo,
		illRepo:   illRepo,
	}
}

func (a *ApiHandler) GetEvents(w http.ResponseWriter, r *http.Request, params oapi.GetEventsParams) {
	logParams := map[string]string{"method": "GetEvents"}
	if params.IllTransactionId != nil {
		logParams["IllTransactionId"] = *params.IllTransactionId
	}
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: logParams,
	})
	resp := []oapi.Event{}
	var eventList []events.Event
	var err error
	if params.IllTransactionId != nil {
		eventList, err = a.eventRepo.GetIllTransactionEvents(ctx, *params.IllTransactionId)
	} else {
		eventList, err = a.eventRepo.ListEvents(ctx)
	}
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	for _, event := range eventList {
		resp = append(resp, toApiEvent(event))
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactions(w http.ResponseWriter, r *http.Request) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetIllTransactions"},
	})
	resp := []oapi.IllTransaction{}
	trans, err := a.illRepo.ListIllTransactions(ctx)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	for _, t := range trans {
		resp = append(resp, toApiIllTransaction(t))
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactionsId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetIllTransactionsId", "id": id},
	})
	trans, err := a.illRepo.GetIllTransactionById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	writeJsonResponse(w, toApiIllTransaction(trans))
}

func (a *ApiHandler) GetPeers(w http.ResponseWriter, r *http.Request) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetPeers"},
	})
	resp := []oapi.Peer{}
	peers, err := a.illRepo.ListPeers(ctx)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	for _, p := range peers {
		resp = append(resp, toApiPeer(p))
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) PostPeers(w http.ResponseWriter, r *http.Request) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "PostPeers"},
	})
	var newPeer oapi.Peer
	err := json.NewDecoder(r.Body).Decode(&newPeer)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	if newPeer.Symbol == "" || !strings.Contains(newPeer.Symbol, ":") {
		resp := ErrorMessage{
			Error: "Symbol should be in \"ISIL:SYMBOL\" format but got " + newPeer.Symbol,
		}
		ctx.Logger().Error("error serving api request", "error", err.Error())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	peer, err := a.illRepo.SavePeer(ctx, ill_db.SavePeerParams(toDbPeer(newPeer)))
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiPeer(peer))
}

func (a *ApiHandler) DeletePeersSymbol(w http.ResponseWriter, r *http.Request, symbol string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "DeletePeersSymbol", "symbol": symbol},
	})
	peer, err := a.illRepo.GetPeerBySymbol(ctx, symbol)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	err = a.illRepo.DeletePeer(ctx, peer.ID)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ApiHandler) GetPeersSymbol(w http.ResponseWriter, r *http.Request, symbol string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetPeersSymbol", "symbol": symbol},
	})
	peer, err := a.illRepo.GetPeerBySymbol(ctx, symbol)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	writeJsonResponse(w, toApiPeer(peer))
}

func (a *ApiHandler) PutPeersSymbol(w http.ResponseWriter, r *http.Request, symbol string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "PutPeersSymbol", "symbol": symbol},
	})
	peer, err := a.illRepo.GetPeerBySymbol(ctx, symbol)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	var update oapi.Peer
	err = json.NewDecoder(r.Body).Decode(&update)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	if update.Name != "" {
		peer.Name = update.Name
	}
	if update.Url != "" {
		peer.Url = update.Url
	}
	peer.RefreshPolicy = toDbRefreshPolicy(update.RefreshPolicy)
	peer, err = a.illRepo.SavePeer(ctx, ill_db.SavePeerParams(peer))
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, toApiPeer(peer))
}

func writeJsonResponse(w http.ResponseWriter, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

type ErrorMessage struct {
	Error string `json:"error"`
}

func addInternalError(ctx extctx.ExtendedContext, w http.ResponseWriter, err error) {
	resp := ErrorMessage{
		Error: err.Error(),
	}
	ctx.Logger().Error("error serving api request", "error", err.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(resp)
}

func addNotFoundError(w http.ResponseWriter) {
	resp := ErrorMessage{
		Error: "not found",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(resp)
}

func toApiEvent(event events.Event) oapi.Event {
	api := oapi.Event{
		ID:               event.ID,
		Timestamp:        event.Timestamp.Time,
		IllTransactionID: event.IllTransactionID,
		EventType:        string(event.EventType),
		EventName:        string(event.EventName),
		EventStatus:      string(event.EventStatus),
		ParentID:         toString(event.ParentID),
	}
	eventData := utils.Must(structToMap(event.EventData))
	api.EventData = &eventData
	resultData := utils.Must(structToMap(event.ResultData))
	api.ResultData = &resultData
	return api
}

func structToMap(obj interface{}) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	val := reflect.ValueOf(obj)
	typ := reflect.TypeOf(obj)

	if val.Kind() == reflect.Ptr {
		val = val.Elem()
		typ = typ.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("input is not a struct")
	}

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldName := typ.Field(i).Name
		result[fieldName] = field.Interface()
	}

	return result, nil
}

func toApiIllTransaction(trans ill_db.IllTransaction) oapi.IllTransaction {
	api := oapi.IllTransaction{
		ID:        trans.ID,
		Timestamp: trans.Timestamp.Time,
	}
	if trans.RequesterSymbol.Valid {
		api.RequesterSymbol = trans.RequesterSymbol.String
	}
	if trans.RequesterID.Valid {
		api.RequesterID = trans.RequesterID.String
	}
	if trans.LastRequesterAction.Valid {
		api.LastRequesterAction = trans.LastRequesterAction.String
	}
	if trans.PrevRequesterAction.Valid {
		api.PrevRequesterAction = trans.PrevRequesterAction.String
	}
	if trans.SupplierSymbol.Valid {
		api.SupplierSymbol = trans.SupplierSymbol.String
	}
	if trans.RequesterRequestID.Valid {
		api.RequesterRequestID = trans.RequesterRequestID.String
	}
	if trans.SupplierRequestID.Valid {
		api.SupplierRequestID = trans.SupplierRequestID.String
	}
	if trans.LastSupplierStatus.Valid {
		api.LastSupplierStatus = trans.LastSupplierStatus.String
	}
	if trans.PrevSupplierStatus.Valid {
		api.PrevSupplierStatus = trans.PrevSupplierStatus.String
	}
	api.IllTransactionData = toApiIllTransactionData(trans.IllTransactionData)
	return api
}

func toApiIllTransactionData(trans ill_db.IllTransactionData) map[string]interface{} {
	api := make(map[string]interface{})
	api["BibliographicInfo"] = trans.BibliographicInfo
	if trans.PublicationInfo != nil {
		api["PublicationInfo"] = trans.PublicationInfo
	}
	if trans.ServiceInfo != nil {
		api["ServiceInfo"] = trans.ServiceInfo
	}
	if trans.SupplierInfo != nil {
		api["SupplierInfo"] = trans.SupplierInfo
	}
	if trans.RequestedDeliveryInfo != nil {
		api["RequestedDeliveryInfo"] = trans.RequestedDeliveryInfo
	}
	if trans.RequestingAgencyInfo != nil {
		api["RequestingAgencyInfo"] = trans.RequestingAgencyInfo
	}
	if trans.PatronInfo != nil {
		api["PatronInfo"] = trans.PatronInfo
	}
	if trans.BillingInfo != nil {
		api["BillingInfo"] = trans.BillingInfo
	}
	if trans.DeliveryInfo != nil {
		api["DeliveryInfo"] = trans.DeliveryInfo
	}
	if trans.ReturnInfo != nil {
		api["ReturnInfo"] = trans.ReturnInfo
	}
	return api
}

func toApiPeer(peer ill_db.Peer) oapi.Peer {
	return oapi.Peer{
		ID:            peer.ID,
		Symbol:        peer.Symbol,
		Name:          peer.Name,
		Url:           peer.Url,
		RefreshPolicy: toApiPeerRefreshPolicy(peer.RefreshPolicy),
		Vendor:        peer.Vendor,
	}
}

func toApiPeerRefreshPolicy(policy ill_db.RefreshPolicy) oapi.PeerRefreshPolicy {
	if policy == ill_db.RefreshPolicyNever {
		return oapi.Never
	} else {
		return oapi.Transaction
	}
}

func toDbPeer(peer oapi.Peer) ill_db.Peer {
	db := ill_db.Peer{
		ID:            peer.ID,
		Symbol:        peer.Symbol,
		Name:          peer.Name,
		Url:           peer.Url,
		Vendor:        peer.Vendor,
		RefreshPolicy: toDbRefreshPolicy(peer.RefreshPolicy),
		RefreshTime: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
	}
	if db.ID == "" {
		db.ID = uuid.New().String()
	}
	return db
}

func toDbRefreshPolicy(policy oapi.PeerRefreshPolicy) ill_db.RefreshPolicy {
	if policy == oapi.Never {
		return ill_db.RefreshPolicyNever
	} else {
		return ill_db.RefreshPolicyTransaction
	}
}

func toString(text pgtype.Text) *string {
	if text.Valid {
		return &text.String
	} else {
		return nil
	}
}
