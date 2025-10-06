package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/service"

	"github.com/indexdata/go-utils/utils"

	"github.com/google/uuid"
	icql "github.com/indexdata/cql-go/cql"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/oapi"
	"github.com/indexdata/crosslink/broker/vcs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var ILL_TRANSACTIONS_PATH = "/ill_transactions"
var EVENTS_PATH = "/events"
var LOCATED_SUPPLIERS_PATH = "/located_suppliers"
var PEERS_PATH = "/peers"
var ILL_TRANSACTION_QUERY = "ill_transaction_id="
var LIMIT_DEFAULT int32 = 10
var ARCHIVE_PROCESS_STARTED = "Archive process started"

type ApiHandler struct {
	limitDefault   int32
	eventRepo      events.EventRepo
	illRepo        ill_db.IllRepo
	tenantToSymbol string // non-empty if in /broker mode
}

func NewApiHandler(eventRepo events.EventRepo, illRepo ill_db.IllRepo, tenentToSymbol string, limitDefault int32) ApiHandler {
	return ApiHandler{
		eventRepo:      eventRepo,
		illRepo:        illRepo,
		tenantToSymbol: tenentToSymbol,
		limitDefault:   limitDefault,
	}
}

func (a *ApiHandler) isTenantMode() bool {
	return a.tenantToSymbol != ""
}

func (a *ApiHandler) getSymbolFromTenant(tenant string) string {
	return strings.ReplaceAll(a.tenantToSymbol, "{tenant}", strings.ToUpper(tenant))
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
	return trans.RequesterSymbol.String == a.getSymbolFromTenant(*tenant)
}

func (a *ApiHandler) getIllTranFromParams(ctx common.ExtendedContext, w http.ResponseWriter,
	okapiTenant *string, requesterSymbol *string, requesterReqId *oapi.RequesterRequestId,
	illTransactionId *oapi.IllTransactionId) (*ill_db.IllTransaction, error) {
	var tran ill_db.IllTransaction
	var err error
	if requesterReqId != nil {
		tran, err = a.illRepo.GetIllTransactionByRequesterRequestId(ctx, pgtype.Text{
			String: *requesterReqId,
			Valid:  true,
		})
	} else if illTransactionId != nil {
		tran, err = a.illRepo.GetIllTransactionById(ctx, *illTransactionId)
	} else {
		err = fmt.Errorf("either requesterReqId or illTransactionId should be provided")
		addBadRequestError(ctx, w, err)
		return nil, err
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		addInternalError(ctx, w, err)
		return nil, err
	}
	if !a.isOwner(&tran, okapiTenant, requesterSymbol) {
		return nil, nil
	}
	return &tran, nil
}

func (a *ApiHandler) Get(w http.ResponseWriter, r *http.Request) {
	var index oapi.Index
	index.Revision = vcs.GetCommit()
	index.Signature = vcs.GetSignature()
	index.Links.IllTransactionsLink = toLink(r, ILL_TRANSACTIONS_PATH, "", "")
	index.Links.EventsLink = toLink(r, EVENTS_PATH, "", "")
	index.Links.LocatedSuppliersLink = toLink(r, LOCATED_SUPPLIERS_PATH, "", "")
	index.Links.PeersLink = toLink(r, PEERS_PATH, "", "")
	writeJsonResponse(w, index)
}

func (a *ApiHandler) GetEvents(w http.ResponseWriter, r *http.Request, params oapi.GetEventsParams) {
	logParams := map[string]string{"method": "GetEvents"}
	if params.IllTransactionId != nil {
		logParams["IllTransactionId"] = *params.IllTransactionId
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: logParams,
	})
	tran, err := a.getIllTranFromParams(ctx, w, params.XOkapiTenant, params.RequesterSymbol,
		params.RequesterReqId, params.IllTransactionId)
	if err != nil {
		return
	}
	var resp oapi.Events
	resp.Items = make([]oapi.Event, 0)
	if tran == nil {
		writeJsonResponse(w, resp)
		return
	}
	var fullCount int64
	var eventList []events.Event
	eventList, fullCount, err = a.eventRepo.GetIllTransactionEvents(ctx, tran.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		addInternalError(ctx, w, err)
		return
	}
	resp.About.Count = fullCount
	for _, event := range eventList {
		resp.Items = append(resp.Items, toApiEvent(event))
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactions(w http.ResponseWriter, r *http.Request, params oapi.GetIllTransactionsParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetIllTransactions"},
	})
	var resp oapi.IllTransactions
	resp.Items = make([]oapi.IllTransaction, 0)

	cql := params.Cql

	limit := a.limitDefault
	if params.Limit != nil {
		limit = *params.Limit
	}
	var offset int32 = 0
	if params.Offset != nil {
		offset = *params.Offset
	}
	var fullCount int64
	if params.RequesterReqId != nil {
		tran, err := a.getIllTranFromParams(ctx, w, params.XOkapiTenant, params.RequesterSymbol,
			params.RequesterReqId, nil)
		if err != nil {
			return
		}
		if tran != nil {
			fullCount = 1
			resp.Items = append(resp.Items, toApiIllTransaction(r, *tran))
		}
	} else if a.isTenantMode() {
		var tenantSymbol string
		if params.XOkapiTenant != nil {
			tenantSymbol = a.getSymbolFromTenant(*params.XOkapiTenant)
		} else if params.RequesterSymbol != nil {
			tenantSymbol = *params.RequesterSymbol
		}
		if tenantSymbol == "" {
			writeJsonResponse(w, resp)
			return
		}
		dbparams := ill_db.GetIllTransactionsByRequesterSymbolParams{
			Limit:  limit,
			Offset: offset,
			RequesterSymbol: pgtype.Text{
				String: tenantSymbol,
				Valid:  true,
			},
		}
		var trans []ill_db.IllTransaction
		var err error
		trans, fullCount, err = a.illRepo.GetIllTransactionsByRequesterSymbol(ctx, dbparams, cql)
		if err != nil { //DB error
			addInternalError(ctx, w, err)
			return
		}
		for _, t := range trans {
			resp.Items = append(resp.Items, toApiIllTransaction(r, t))
		}
	} else {
		dbparams := ill_db.ListIllTransactionsParams{
			Limit:  limit,
			Offset: offset,
		}
		var trans []ill_db.IllTransaction
		var err error
		trans, fullCount, err = a.illRepo.ListIllTransactions(ctx, dbparams, cql)
		if err != nil { //DB error
			addInternalError(ctx, w, err)
			return
		}
		for _, t := range trans {
			resp.Items = append(resp.Items, toApiIllTransaction(r, t))
		}
	}
	resp.About.Count = fullCount
	if offset > 0 {
		pOffset := offset - limit
		if pOffset < 0 {
			pOffset = 0
		}
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.Itoa(int(pOffset))}
		link := toLinkUrlValues(r, urlValues)
		resp.About.PrevLink = &link
	}
	if fullCount > int64(limit+offset) {
		noffset := offset + limit
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.Itoa(int(noffset))}
		link := toLinkUrlValues(r, urlValues)
		resp.About.NextLink = &link
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactionsId(w http.ResponseWriter, r *http.Request, id string, params oapi.GetIllTransactionsIdParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetIllTransactionsId", "id": id},
	})
	tran, err := a.getIllTranFromParams(ctx, w, params.XOkapiTenant, params.RequesterSymbol,
		nil, &id)
	if err != nil {
		return
	}
	if tran == nil {
		addNotFoundError(w)
		return
	}
	writeJsonResponse(w, toApiIllTransaction(r, *tran))
}

func (a *ApiHandler) DeleteIllTransactionsId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
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

func (a *ApiHandler) returnHttpError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
	// check if error is cql.ParserError
	if cqlErr, ok := err.(*icql.ParseError); ok {
		addBadRequestError(ctx, w, fmt.Errorf("cql parser error: %s", cqlErr.Error()))
		return
	}

	if cqlErr, ok := err.(*pgcql.PgError); ok {
		addBadRequestError(ctx, w, fmt.Errorf("pgcql error: %s", cqlErr.Error()))
		return
	}
	addInternalError(ctx, w, err)
}

func (a *ApiHandler) GetPeers(w http.ResponseWriter, r *http.Request, params oapi.GetPeersParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPeers"},
	})
	dbparams := ill_db.ListPeersParams{
		Limit:  a.limitDefault,
		Offset: 0,
	}
	if params.Limit != nil {
		dbparams.Limit = *params.Limit
	}
	if params.Offset != nil {
		dbparams.Offset = *params.Offset
	}
	peers, count, err := a.illRepo.ListPeers(ctx, dbparams, params.Cql)
	if err != nil {
		a.returnHttpError(ctx, w, err)
		return
	}
	var resp oapi.Peers
	resp.Items = make([]oapi.Peer, 0)
	resp.About.Count = count
	for _, p := range peers {
		symbols, e := a.illRepo.GetSymbolsByPeerId(ctx, p.ID)
		if e != nil {
			addInternalError(ctx, w, e)
			return
		}
		branchSymbols, e := a.illRepo.GetBranchSymbolsByPeerId(ctx, p.ID)
		if e != nil {
			addInternalError(ctx, w, e)
			return
		}
		resp.Items = append(resp.Items, toApiPeer(p, symbols, branchSymbols))
	}

	if dbparams.Offset > 0 {
		pOffset := dbparams.Offset - dbparams.Limit
		if pOffset < 0 {
			pOffset = 0
		}
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.Itoa(int(pOffset))}
		link := toLinkUrlValues(r, urlValues)
		resp.About.PrevLink = &link
	}
	if count > int64(dbparams.Limit+dbparams.Offset) {
		noffset := dbparams.Offset + dbparams.Limit
		urlValues := r.URL.Query()
		urlValues["offset"] = []string{strconv.Itoa(int(noffset))}
		link := toLinkUrlValues(r, urlValues)
		resp.About.NextLink = &link
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) PostPeers(w http.ResponseWriter, r *http.Request) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
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
	dbPeer := toDbPeer(newPeer)
	var peer ill_db.Peer
	var symbols = []ill_db.Symbol{}
	var branchSymbols = []ill_db.BranchSymbol{}
	err = a.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		peer, err = repo.SavePeer(ctx, ill_db.SavePeerParams(dbPeer))
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
		if newPeer.BranchSymbols != nil {
			for _, s := range *newPeer.BranchSymbols {
				sym, e := repo.SaveBranchSymbol(ctx, ill_db.SaveBranchSymbolParams{
					SymbolValue: s,
					PeerID:      peer.ID,
				})
				if e != nil {
					return e
				}
				branchSymbols = append(branchSymbols, sym)
			}
		}
		return nil
	})

	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiPeer(peer, symbols, branchSymbols))
}

func (a *ApiHandler) DeletePeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
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
		err = repo.DeleteBranchSymbolByPeerId(ctx, peer.ID)
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
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
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
	branchSymbols, err := a.illRepo.GetBranchSymbolsByPeerId(ctx, peer.ID)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, toApiPeer(peer, symbols, branchSymbols))
}

func (a *ApiHandler) PutPeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
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
	peer.Name = update.Name
	peer.Url = update.Url
	if update.HttpHeaders != nil {
		peer.HttpHeaders = *update.HttpHeaders
	} else {
		peer.HttpHeaders = make(map[string]string)
	}
	if update.CustomData != nil {
		peer.CustomData = *update.CustomData
	} else {
		peer.CustomData = make(map[string]interface{})
	}
	if update.BrokerMode != "" {
		peer.BrokerMode = string(update.BrokerMode)
	}
	if update.Vendor != "" {
		peer.Vendor = update.Vendor
	}
	peer.RefreshPolicy = toDbRefreshPolicy(update.RefreshPolicy)
	if update.BorrowsCount != nil {
		peer.BorrowsCount = *update.BorrowsCount
	}
	if update.LoansCount != nil {
		peer.LoansCount = *update.LoansCount
	}
	var symbols = []ill_db.Symbol{}
	var branchSymbols = []ill_db.BranchSymbol{}
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
		err = repo.DeleteBranchSymbolByPeerId(ctx, peer.ID)
		if err != nil {
			return err
		}
		if update.BranchSymbols != nil {
			for _, s := range *update.BranchSymbols {
				sym, e := repo.SaveBranchSymbol(ctx, ill_db.SaveBranchSymbolParams{
					SymbolValue: s,
					PeerID:      peer.ID,
				})
				if e != nil {
					return e
				}
				branchSymbols = append(branchSymbols, sym)
			}
		}
		return nil
	})

	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, toApiPeer(peer, symbols, branchSymbols))
}

func (a *ApiHandler) GetLocatedSuppliers(w http.ResponseWriter, r *http.Request, params oapi.GetLocatedSuppliersParams) {
	logParams := map[string]string{"method": "GetLocatedSuppliers"}
	if params.IllTransactionId != nil {
		logParams["IllTransactionId"] = *params.IllTransactionId
	}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: logParams,
	})
	tran, err := a.getIllTranFromParams(ctx, w, params.XOkapiTenant, params.RequesterSymbol,
		params.RequesterReqId, params.IllTransactionId)
	if err != nil {
		return
	}
	var resp oapi.LocatedSuppliers
	resp.Items = make([]oapi.LocatedSupplier, 0)
	if tran == nil {
		writeJsonResponse(w, resp)
		return
	}
	var supList []ill_db.LocatedSupplier
	var count int64
	supList, count, err = a.illRepo.GetLocatedSuppliersByIllTransaction(ctx, tran.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { //DB error
		addInternalError(ctx, w, err)
		return
	}
	resp.About.Count = count
	for _, supplier := range supList {
		resp.Items = append(resp.Items, toApiLocatedSupplier(r, supplier))
	}
	writeJsonResponse(w, resp)
}

func (a *ApiHandler) PostArchiveIllTransactions(w http.ResponseWriter, r *http.Request, params oapi.PostArchiveIllTransactionsParams) {
	logParams := map[string]string{"method": "PostArchiveIllTransactions", "ArchiveDelay": params.ArchiveDelay, "ArchiveStatus": params.ArchiveStatus}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: logParams,
	})
	err := service.Archive(ctx, a.illRepo, params.ArchiveStatus, params.ArchiveDelay, true)
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	writeJsonResponse(w, oapi.StatusMessage{
		Status: ARCHIVE_PROCESS_STARTED,
	})
}

func writeJsonResponse(w http.ResponseWriter, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

type ErrorMessage struct {
	Error string `json:"error"`
}

func deleteIllTransaction(ctx common.ExtendedContext, illRepo ill_db.IllRepo, eventRepo events.EventRepo, transId string) error {
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

func addInternalError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
	resp := ErrorMessage{
		Error: err.Error(),
	}
	ctx.Logger().Error("error serving api request", "error", err.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(resp)
}

func addBadRequestError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
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

func toApiPeer(peer ill_db.Peer, symbols []ill_db.Symbol, branchSymbols []ill_db.BranchSymbol) oapi.Peer {
	list := make([]string, len(symbols))
	for i, s := range symbols {
		list[i] = s.SymbolValue
	}
	var branchList *[]string
	if len(branchSymbols) > 0 {
		branchList = new([]string)
		for _, s := range branchSymbols {
			*branchList = append(*branchList, s.SymbolValue)
		}
	}
	if peer.Vendor == "" {
		peer.Vendor = string(adapter.GetVendorFromUrl(peer.Url))
	}
	if peer.BrokerMode == "" {
		peer.BrokerMode = string(adapter.GetBrokerMode(adapter.GetVendorFromUrl(peer.Url)))
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
		HttpHeaders:   &peer.HttpHeaders,
		BrokerMode:    toApiBrokerMode(peer.BrokerMode),
		BranchSymbols: branchList,
	}
}

func toApiPeerRefreshPolicy(policy ill_db.RefreshPolicy) oapi.PeerRefreshPolicy {
	if policy == ill_db.RefreshPolicyNever {
		return oapi.Never
	} else {
		return oapi.Transaction
	}
}

func toApiBrokerMode(brokerMode string) oapi.PeerBrokerMode {
	if brokerMode == string(oapi.Transparent) {
		return oapi.Transparent
	} else if brokerMode == string(oapi.Translucent) {
		return oapi.Translucent
	} else {
		return oapi.Opaque
	}
}

func toDbPeer(peer oapi.Peer) ill_db.Peer {
	customData := make(map[string]interface{})
	if peer.CustomData != nil {
		customData = *peer.CustomData
	}
	httpHeaders := make(map[string]string)
	if peer.HttpHeaders != nil {
		httpHeaders = *peer.HttpHeaders
	}
	db := ill_db.Peer{
		ID:            peer.ID,
		Name:          peer.Name,
		Url:           peer.Url,
		Vendor:        peer.Vendor,
		BrokerMode:    string(peer.BrokerMode),
		RefreshPolicy: toDbRefreshPolicy(peer.RefreshPolicy),
		RefreshTime: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		CustomData:  customData,
		HttpHeaders: httpHeaders,
	}
	if peer.LoansCount != nil {
		db.LoansCount = *peer.LoansCount
	}
	if peer.BorrowsCount != nil {
		db.BorrowsCount = *peer.BorrowsCount
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

func toLinkUrlValues(r *http.Request, urlValues url.Values) string {
	return toLinkPath(r, r.URL.Path, urlValues.Encode())
}

func toLink(r *http.Request, path string, id string, query string) string {
	if strings.Contains(r.RequestURI, "/broker/") {
		path = "/broker" + path
	}
	if id != "" {
		path = path + "/" + id
	}
	return toLinkPath(r, path, query)
}

func toLinkPath(r *http.Request, path string, query string) string {
	if query != "" {
		path = path + "?" + query
	}
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
	return urlScheme + "://" + urlHost + path
}
