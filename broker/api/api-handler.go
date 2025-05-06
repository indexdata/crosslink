package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/indexdata/go-utils/utils"

	"github.com/google/uuid"
	icql "github.com/indexdata/cql-go/cql"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var EVENTS_PATH = "/events"
var LOCATED_SUPPLIERS_PATH = "/located_suppliers"
var PEERS_PATH = "/peers"
var ILL_TRANSACTION_QUERY = "ill_transaction_id="
var LIMIT_DEFAULT int32 = 10

type ApiHandler struct {
	eventRepo      events.EventRepo
	illRepo        ill_db.IllRepo
	tenantToSymbol string // non-empty if in /broker mode
}

// would have hoped that this would be in the oapi package already
type IllTransactionsResponse struct {
	ResultInfo oapi.ResultInfo       `json:"resultInfo"`
	Items      []oapi.IllTransaction `json:"items"`
}

func NewApiHandler(eventRepo events.EventRepo, illRepo ill_db.IllRepo, tenentToSymbol string) ApiHandler {
	return ApiHandler{
		eventRepo:      eventRepo,
		illRepo:        illRepo,
		tenantToSymbol: tenentToSymbol,
	}
}

func (a *ApiHandler) isTenantMode() bool {
	return a.tenantToSymbol != ""
}

func (a *ApiHandler) isOwner(trans *ill_db.IllTransaction, tenant *string, requesterSymbol *string) bool {
	if tenant == nil && requesterSymbol != nil {
		return trans.RequesterSymbol.String == *requesterSymbol
	}
	if !a.isTenantMode() {
		return true
	}
	if tenant == nil {
		return false
	}
	tenantSymbol := strings.ReplaceAll(a.tenantToSymbol, "{tenant}", strings.ToUpper(*tenant))
	return trans.RequesterSymbol.String == tenantSymbol
}

func (a *ApiHandler) getIllTranFromParams(ctx extctx.ExtendedContext,
	requesterReqId *oapi.RequesterRequestId, illTransactionId *oapi.IllTransactionId) (ill_db.IllTransaction, error) {
	if requesterReqId != nil {
		return a.illRepo.GetIllTransactionByRequesterRequestId(ctx, pgtype.Text{
			String: *requesterReqId,
			Valid:  true,
		})
	}
	return a.illRepo.GetIllTransactionById(ctx, *illTransactionId)
}

func (a *ApiHandler) GetEvents(w http.ResponseWriter, r *http.Request, params oapi.GetEventsParams) {
	logParams := map[string]string{"method": "GetEvents"}
	if params.IllTransactionId != nil {
		logParams["IllTransactionId"] = *params.IllTransactionId
	}
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: logParams,
	})
	if params.RequesterReqId == nil && params.IllTransactionId == nil {
		addBadRequestError(ctx, w, fmt.Errorf("either requesterReqId or illTransactionId should be provided"))
		return
	}
	tran, err := a.getIllTranFromParams(ctx, params.RequesterReqId, params.IllTransactionId)
	if err != nil { //DB error
		if !errors.Is(err, pgx.ErrNoRows) {
			addInternalError(ctx, w, err)
			return
		}
	} else if !a.isOwner(&tran, params.XOkapiTenant, params.RequesterSymbol) {
		tran.ID = ""
	}
	var eventList []events.Event
	if tran.ID != "" {
		eventList, err = a.eventRepo.GetIllTransactionEvents(ctx, tran.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			addInternalError(ctx, w, err)
			return
		}
	}
	resp := []oapi.Event{}
	for _, event := range eventList {
		resp = append(resp, toApiEvent(event))
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactions(w http.ResponseWriter, r *http.Request, params oapi.GetIllTransactionsParams) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetIllTransactions"},
	})
	var resp IllTransactionsResponse
	if params.RequesterReqId != nil {
		tran, err := a.getIllTranFromParams(ctx, params.RequesterReqId, nil)
		if err != nil { //DB error
			if !errors.Is(err, pgx.ErrNoRows) {
				addInternalError(ctx, w, err)
				return
			}
		} else {
			if a.isOwner(&tran, params.XOkapiTenant, params.RequesterSymbol) {
				resp.Items = append(resp.Items, toApiIllTransaction(r, tran))
			}
		}
	} else if a.isTenantMode() {
		var tenantSymbol string
		if params.XOkapiTenant != nil {
			tenantSymbol = strings.ReplaceAll(a.tenantToSymbol, "{tenant}", strings.ToUpper(*params.XOkapiTenant))
		} else if params.RequesterSymbol != nil {
			tenantSymbol = *params.RequesterSymbol
		}
		if tenantSymbol != "" {
			dbparms := ill_db.GetIllTransactionsByRequesterSymbolParams{
				Limit:  LIMIT_DEFAULT,
				Offset: 0,
				RequesterSymbol: pgtype.Text{
					String: tenantSymbol,
					Valid:  true,
				},
			}
			if params.Limit != nil {
				dbparms.Limit = *params.Limit
			}
			if params.Offset != nil {
				dbparms.Offset = *params.Offset
			}
			trans, err := a.illRepo.GetIllTransactionsByRequesterSymbol(ctx, dbparms)
			if err != nil { //DB error
				addInternalError(ctx, w, err)
				return
			}
			for _, t := range trans {
				resp.Items = append(resp.Items, toApiIllTransaction(r, t))
			}
		}
	} else {
		dbparms := ill_db.ListIllTransactionsParams{
			Limit:  LIMIT_DEFAULT,
			Offset: 0,
		}
		if params.Limit != nil {
			dbparms.Limit = *params.Limit
		}
		if params.Offset != nil {
			dbparms.Offset = *params.Offset
		}
		trans, err := a.illRepo.ListIllTransactions(ctx, dbparms)
		if err != nil { //DB error
			addInternalError(ctx, w, err)
			return
		}
		for _, t := range trans {
			resp.Items = append(resp.Items, toApiIllTransaction(r, t))
		}
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactionsId(w http.ResponseWriter, r *http.Request, id string, params oapi.GetIllTransactionsIdParams) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetIllTransactionsId", "id": id},
	})
	trans, err := a.illRepo.GetIllTransactionById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		}
		addInternalError(ctx, w, err)
		return
	}
	if !a.isOwner(&trans, params.XOkapiTenant, params.RequesterSymbol) {
		addNotFoundError(w)
		return
	}
	if trans.ID == "" {
		addNotFoundError(w)
		return
	}
	writeJsonResponse(w, toApiIllTransaction(r, trans))
}

func (a *ApiHandler) DeleteIllTransactionsId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "DeleteIllTransactionsId", "id": id},
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
	err = a.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		return deleteIllTransaction(ctx, repo, a.eventRepo, trans.ID)
	})
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ApiHandler) GetPeers(w http.ResponseWriter, r *http.Request, params oapi.GetPeersParams) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetPeers"},
	})
	dbparams := ill_db.ListPeersParams{
		Limit:  LIMIT_DEFAULT,
		Offset: 0,
	}
	if params.Limit != nil {
		dbparams.Limit = *params.Limit
	}
	if params.Offset != nil {
		dbparams.Offset = *params.Offset
	}
	peers, err := a.illRepo.ListPeers(ctx, dbparams)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	resp := []oapi.Peer{}
	for _, p := range peers {
		symbols, e := a.illRepo.GetSymbolsByPeerId(ctx, p.ID)
		if e != nil {
			addInternalError(ctx, w, e)
			return
		}
		resp = append(resp, toApiPeer(p, symbols))
	}
	resp, err = filterPeers(params.Cql, resp)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, resp)
}

func filterPeers(cql *string, peers []oapi.Peer) ([]oapi.Peer, error) {
	var filtered []oapi.Peer
	if cql != nil && *cql != "" {
		var p icql.Parser
		query, err := p.Parse(*cql)
		if err != nil {
			return peers, err
		}
		for _, entry := range peers {
			match, err := matchQuery(query, entry.Symbols)
			if err != nil {
				return peers, err
			}
			if !match {
				continue
			}
			filtered = append(filtered, entry)
		}
	} else {
		return peers, nil
	}
	return filtered, nil
}

func matchQuery(query icql.Query, symbols []string) (bool, error) {
	return matchClause(&query.Clause, symbols)
}
func matchClause(clause *icql.Clause, symbols []string) (bool, error) {
	if symbols == nil {
		return false, nil
	}
	if clause.SearchClause != nil {
		sc := clause.SearchClause
		if sc.Index != "symbol" {
			return false, fmt.Errorf("unsupported index %s", sc.Index)
		}
		tSymbols := strings.Split(sc.Term, " ")
		switch sc.Relation {
		case icql.ANY:
			for _, t := range tSymbols {
				for _, s := range symbols {
					if s == t {
						return true, nil
					}
				}
			}
			return false, nil
		case icql.ALL:
			for _, t := range tSymbols {
				found := false
				for _, s := range symbols {
					if s == t {
						found = true
					}
				}
				if !found {
					return false, nil
				}
			}
			return true, nil
		case "=":
			// all match match in order
			if len(tSymbols) != len(symbols) {
				return false, nil
			}
			for i, t := range tSymbols {
				if t != symbols[i] {
					return false, nil
				}
			}
			return true, nil
		}
	}
	if clause.BoolClause != nil {
		bc := clause.BoolClause
		left, err := matchClause(&bc.Left, symbols)
		if err != nil {
			return false, err
		}
		right, err := matchClause(&bc.Right, symbols)
		if err != nil {
			return false, err
		}
		switch bc.Operator {
		case icql.AND:
			return left && right, nil
		case icql.OR:
			return left || right, nil
		case icql.NOT:
			return left && !right, nil
		default:
			return false, fmt.Errorf("unsupported operator %s", bc.Operator)
		}
	}
	return false, nil
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
	_, err = a.illRepo.GetPeerById(ctx, newPeer.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		addInternalError(ctx, w, err)
		return
	} else if err == nil {
		addBadRequestError(ctx, w, fmt.Errorf("ID %v is already used", newPeer.ID))
		return
	}
	for _, s := range newPeer.Symbols {
		if !strings.Contains(s, ":") {
			addBadRequestError(ctx, w, fmt.Errorf("symbol should be in \"ISIL:SYMBOL\" format but got %v", s))
			return
		}
	}
	var peer ill_db.Peer
	var symbols = []ill_db.Symbol{}
	err = a.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		peer, err = repo.SavePeer(ctx, ill_db.SavePeerParams(toDbPeer(newPeer)))
		if err != nil {
			return err
		}
		for _, s := range newPeer.Symbols {
			sym, e := repo.SaveSymbol(ctx, ill_db.SaveSymbolParams{
				SymbolValue: s,
				PeerID:      peer.ID,
			})
			if e != nil {
				return e
			}
			symbols = append(symbols, sym)
		}
		return nil
	})

	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiPeer(peer, symbols))
}

func (a *ApiHandler) DeletePeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "DeletePeersSymbol", "id": id},
	})
	err := a.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		peer, err := repo.GetPeerById(ctx, id)
		if err != nil {
			return err
		}
		trans, err := repo.GetIllTransactionByRequesterId(ctx, pgtype.Text{
			String: peer.ID,
			Valid:  true,
		})
		if err != nil {
			return err
		}
		for _, t := range trans {
			err = deleteIllTransaction(ctx, repo, a.eventRepo, t.ID)
			if err != nil {
				return err
			}
		}
		suppliers, err := a.illRepo.GetLocatedSupplierByPeerId(ctx, peer.ID)
		if err != nil {
			return err
		}
		for _, s := range suppliers {
			err = deleteIllTransaction(ctx, repo, a.eventRepo, s.IllTransactionID)
			if err != nil {
				return err
			}
		}
		err = repo.DeleteSymbolByPeerId(ctx, peer.ID)
		if err != nil {
			return err
		}
		err = repo.DeletePeer(ctx, peer.ID)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ApiHandler) GetPeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "GetPeersSymbol", "id": id},
	})
	peer, err := a.illRepo.GetPeerById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	symbols, err := a.illRepo.GetSymbolsByPeerId(ctx, peer.ID)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, toApiPeer(peer, symbols))
}

func (a *ApiHandler) PutPeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: map[string]string{"method": "PutPeersSymbol", "id": id},
	})
	peer, err := a.illRepo.GetPeerById(ctx, id)
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
	var symbols = []ill_db.Symbol{}
	err = a.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		peer, err = repo.SavePeer(ctx, ill_db.SavePeerParams(peer))
		if err != nil {
			return err
		}
		err = repo.DeleteSymbolByPeerId(ctx, peer.ID)
		if err != nil {
			return err
		}
		for _, s := range update.Symbols {
			sym, e := repo.SaveSymbol(ctx, ill_db.SaveSymbolParams{
				SymbolValue: s,
				PeerID:      peer.ID,
			})
			if e != nil {
				return e
			}
			symbols = append(symbols, sym)
		}
		return nil
	})

	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, toApiPeer(peer, symbols))
}

func (a *ApiHandler) GetLocatedSuppliers(w http.ResponseWriter, r *http.Request, params oapi.GetLocatedSuppliersParams) {
	logParams := map[string]string{"method": "GetLocatedSuppliers"}
	if params.IllTransactionId != nil {
		logParams["IllTransactionId"] = *params.IllTransactionId
	}
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), &extctx.LoggerArgs{
		Other: logParams,
	})
	if params.RequesterReqId == nil && params.IllTransactionId == nil {
		addBadRequestError(ctx, w, fmt.Errorf("either requesterReqId or illTransactionId should be provided"))
		return
	}
	tran, err := a.getIllTranFromParams(ctx, params.RequesterReqId, params.IllTransactionId)
	if err != nil { //DB error
		if !errors.Is(err, pgx.ErrNoRows) {
			addInternalError(ctx, w, err)
			return
		}
	} else if !a.isOwner(&tran, params.XOkapiTenant, params.RequesterSymbol) {
		tran.ID = ""
	}
	var supList []ill_db.LocatedSupplier
	if tran.ID != "" {
		supList, err = a.illRepo.GetLocatedSupplierByIllTransition(ctx, tran.ID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) { //DB error
			addInternalError(ctx, w, err)
			return
		}
	}
	resp := []oapi.LocatedSupplier{}
	for _, supplier := range supList {
		resp = append(resp, toApiLocatedSupplier(r, supplier))
	}
	writeJsonResponse(w, resp)
}

func writeJsonResponse(w http.ResponseWriter, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

type ErrorMessage struct {
	Error string `json:"error"`
}

func deleteIllTransaction(ctx extctx.ExtendedContext, illRepo ill_db.IllRepo, eventRepo events.EventRepo, transId string) error {
	inErr := eventRepo.DeleteEventsByIllTransaction(ctx, transId)
	if inErr != nil {
		return inErr
	}
	inErr = illRepo.DeleteLocatedSupplierByIllTransaction(ctx, transId)
	if inErr != nil {
		return inErr
	}
	return illRepo.DeleteIllTransaction(ctx, transId)
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

func addBadRequestError(ctx extctx.ExtendedContext, w http.ResponseWriter, err error) {
	resp := ErrorMessage{
		Error: err.Error(),
	}
	ctx.Logger().Error("error serving api request", "error", err.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
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

func toApiLocatedSupplier(r *http.Request, sup ill_db.LocatedSupplier) oapi.LocatedSupplier {
	return oapi.LocatedSupplier{
		ID:                sup.ID,
		IllTransactionID:  sup.IllTransactionID,
		SupplierID:        sup.SupplierID,
		SupplierSymbol:    sup.SupplierSymbol,
		Ordinal:           sup.Ordinal,
		SupplierStatus:    toString(sup.SupplierStatus),
		PrevAction:        toString(sup.PrevAction),
		PrevStatus:        toString(sup.PrevStatus),
		LastAction:        toString(sup.LastAction),
		LastStatus:        toString(sup.LastStatus),
		LocalID:           toString(sup.LocalID),
		PrevReason:        toString(sup.PrevReason),
		LastReason:        toString(sup.LastReason),
		SupplierRequestID: toString(sup.SupplierRequestID),
		SupplierPeerLink:  toLink(r, PEERS_PATH, sup.SupplierID, ""),
	}
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

func toApiIllTransaction(r *http.Request, trans ill_db.IllTransaction) oapi.IllTransaction {
	api := oapi.IllTransaction{
		ID:        trans.ID,
		Timestamp: trans.Timestamp.Time,
	}
	api.RequesterSymbol = getString(trans.RequesterSymbol)

	api.RequesterID = getString(trans.RequesterID)
	api.LastRequesterAction = getString(trans.LastRequesterAction)
	api.PrevRequesterAction = getString(trans.PrevRequesterAction)
	api.SupplierSymbol = getString(trans.SupplierSymbol)
	api.RequesterRequestID = getString(trans.RequesterRequestID)
	api.SupplierRequestID = getString(trans.SupplierRequestID)
	api.LastSupplierStatus = getString(trans.LastSupplierStatus)
	api.PrevSupplierStatus = getString(trans.PrevSupplierStatus)
	api.EventsLink = toLink(r, EVENTS_PATH, "", ILL_TRANSACTION_QUERY+trans.ID)
	api.LocatedSuppliersLink = toLink(r, LOCATED_SUPPLIERS_PATH, "", ILL_TRANSACTION_QUERY+trans.ID)
	if trans.RequesterID.Valid {
		api.RequesterPeerLink = toLink(r, PEERS_PATH, trans.RequesterID.String, "")
	}
	api.IllTransactionData = utils.Must(structToMap(trans.IllTransactionData))
	return api
}

func getString(value pgtype.Text) string {
	if value.Valid {
		return value.String
	} else {
		return ""
	}
}

func toApiPeer(peer ill_db.Peer, symbols []ill_db.Symbol) oapi.Peer {
	list := make([]string, len(symbols))
	for i, s := range symbols {
		list[i] = s.SymbolValue
	}
	return oapi.Peer{
		ID:            peer.ID,
		Symbols:       list,
		Name:          peer.Name,
		Url:           peer.Url,
		RefreshPolicy: toApiPeerRefreshPolicy(peer.RefreshPolicy),
		Vendor:        peer.Vendor,
		RefreshTime:   &peer.RefreshTime.Time,
		LoansCount:    &peer.LoansCount,
		BorrowsCount:  &peer.BorrowsCount,
		CustomData:    &peer.CustomData,
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

func toLink(r *http.Request, path string, id string, query string) string {
	urlScheme := r.Header.Get("X-Forwarded-Proto")
	if len(urlScheme) == 0 {
		urlScheme = r.URL.Scheme
	}
	if len(urlScheme) == 0 {
		urlScheme = "https"
	}
	urlHost := r.Header.Get("X-Forwarded-Host")
	if len(urlHost) == 0 {
		urlHost = r.URL.Host
	}
	if len(urlHost) == 0 {
		urlHost = r.Host
	}
	if strings.Contains(urlHost, "localhost") {
		urlScheme = "http"
	}
	if strings.Contains(r.RequestURI, "/broker/") {
		path = "/broker" + path
	}
	if id != "" {
		path = path + "/" + id
	}
	if query != "" {
		path = path + "?" + query
	}
	return urlScheme + "://" + urlHost + path
}
