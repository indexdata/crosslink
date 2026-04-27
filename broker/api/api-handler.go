package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/indexdata/crosslink/directory"

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
var PATRON_REQUESTS_PATH = "/patron_requests"
var LIMIT_DEFAULT int32 = 10
var ARCHIVE_PROCESS_STARTED = "Archive process started"

type ApiHandler struct {
	limitDefault   int32
	eventRepo      events.EventRepo
	illRepo        ill_db.IllRepo
	tenantResolver tenant.TenantResolver
}

func NewApiHandler(eventRepo events.EventRepo, illRepo ill_db.IllRepo, tenantResolver tenant.TenantResolver, limitDefault int32) ApiHandler {
	return ApiHandler{
		eventRepo:      eventRepo,
		illRepo:        illRepo,
		tenantResolver: tenantResolver,
		limitDefault:   limitDefault,
	}
}

func (a *ApiHandler) getIllTranFromParams(ctx common.ExtendedContext, w http.ResponseWriter,
	r *http.Request, requesterSymbol *string, requesterReqId *oapi.RequesterRequestId,
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
		AddBadRequestError(ctx, w, err)
		return nil, err
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		AddInternalError(ctx, w, err)
		return nil, err
	}
	tenant, err := a.tenantResolver.Resolve(ctx, r, requesterSymbol)
	if err != nil {
		AddBadRequestError(ctx, w, err)
		return nil, err
	}
	isOwner, err := tenant.IsOwnerOf(tran.RequesterSymbol.String)
	if err != nil {
		AddBadRequestError(ctx, w, err)
		return nil, err
	}
	if isOwner {
		return &tran, nil
	}
	return nil, nil
}

func (a *ApiHandler) Get(w http.ResponseWriter, r *http.Request) {
	var index oapi.Index
	index.Revision = vcs.GetCommit()
	index.Signature = vcs.GetSignature()
	index.Links.IllTransactionsLink = Link(r, Path(ILL_TRANSACTIONS_PATH), nil)
	index.Links.EventsLink = Link(r, Path(EVENTS_PATH), nil)
	index.Links.LocatedSuppliersLink = Link(r, Path(LOCATED_SUPPLIERS_PATH), nil)
	index.Links.PeersLink = Link(r, Path(PEERS_PATH), nil)
	index.Links.PatronRequestsLink = Link(r, Path(PATRON_REQUESTS_PATH), nil)
	WriteJsonResponse(w, index)
}

func (a *ApiHandler) GetEvents(w http.ResponseWriter, r *http.Request, params oapi.GetEventsParams) {
	logParams := map[string]string{"method": "GetEvents"}
	if params.IllTransactionId != nil {
		logParams["IllTransactionId"] = *params.IllTransactionId
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: logParams,
	})
	tran, err := a.getIllTranFromParams(ctx, w, r, params.RequesterSymbol,
		params.RequesterReqId, params.IllTransactionId)
	if err != nil {
		return
	}
	var resp oapi.Events
	resp.Items = make([]oapi.Event, 0)
	if tran == nil {
		WriteJsonResponse(w, resp)
		return
	}
	var fullCount int64
	var eventList []events.Event
	eventList, fullCount, err = a.eventRepo.GetIllTransactionEvents(ctx, tran.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		AddInternalError(ctx, w, err)
		return
	}
	resp.About.Count = fullCount
	for _, event := range eventList {
		resp.Items = append(resp.Items, ToApiEvent(event, event.IllTransactionID, nil))
	}
	WriteJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactions(w http.ResponseWriter, r *http.Request, params oapi.GetIllTransactionsParams) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
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
		tran, err := a.getIllTranFromParams(ctx, w, r, params.RequesterSymbol,
			params.RequesterReqId, nil)
		if err != nil {
			return
		}
		if tran != nil {
			fullCount = 1
			resp.Items = append(resp.Items, toApiIllTransaction(r, *tran))
		}
	} else {
		tenant, err := a.tenantResolver.Resolve(ctx, r, params.RequesterSymbol)
		if err != nil {
			AddBadRequestError(ctx, w, err)
			return
		}
		symbols, err := tenant.GetOwnedSymbols()
		if err != nil {
			AddBadRequestError(ctx, w, err)
			return
		}
		dbparams := ill_db.ListIllTransactionsParams{
			Limit:  limit,
			Offset: offset,
		}
		var trans []ill_db.IllTransaction
		trans, fullCount, err = a.illRepo.ListIllTransactions(ctx, dbparams, cql, symbols)
		if err != nil { //DB error
			AddInternalError(ctx, w, err)
			return
		}
		for _, t := range trans {
			resp.Items = append(resp.Items, toApiIllTransaction(r, t))
		}
	}
	resp.About = CollectAboutData(fullCount, offset, limit, r)
	WriteJsonResponse(w, resp)
}

func (a *ApiHandler) GetIllTransactionsId(w http.ResponseWriter, r *http.Request, id string, params oapi.GetIllTransactionsIdParams) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetIllTransactionsId", "id": id},
	})
	tran, err := a.getIllTranFromParams(ctx, w, r, params.RequesterSymbol,
		nil, &id)
	if err != nil {
		return
	}
	if tran == nil {
		AddNotFoundError(w)
		return
	}
	WriteJsonResponse(w, toApiIllTransaction(r, *tran))
}

func (a *ApiHandler) DeleteIllTransactionsId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: map[string]string{"method": "DeleteIllTransactionsId", "id": id},
	})
	trans, err := a.illRepo.GetIllTransactionById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			AddNotFoundError(w)
			return
		} else {
			AddInternalError(ctx, w, err)
			return
		}
	}
	err = a.illRepo.WithTxFunc(ctx, func(repo ill_db.IllRepo) error {
		return deleteIllTransaction(ctx, repo, a.eventRepo, trans.ID)
	})
	if err != nil {
		AddInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ApiHandler) returnHttpError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
	// check if error is cql.ParserError
	if cqlErr, ok := err.(*icql.ParseError); ok {
		AddBadRequestError(ctx, w, fmt.Errorf("cql parser error: %s", cqlErr.Error()))
		return
	}

	if cqlErr, ok := err.(*pgcql.PgError); ok {
		AddBadRequestError(ctx, w, fmt.Errorf("pgcql error: %s", cqlErr.Error()))
		return
	}
	AddInternalError(ctx, w, err)
}

func (a *ApiHandler) GetPeers(w http.ResponseWriter, r *http.Request, params oapi.GetPeersParams) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
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
	for _, p := range peers {
		symbols, e := a.illRepo.GetSymbolsByPeerId(ctx, p.ID)
		if e != nil {
			AddInternalError(ctx, w, e)
			return
		}
		branchSymbols, e := a.illRepo.GetBranchSymbolsByPeerId(ctx, p.ID)
		if e != nil {
			AddInternalError(ctx, w, e)
			return
		}
		apiPeer := toApiPeer(p, symbols, branchSymbols)
		resp.Items = append(resp.Items, apiPeer)
	}
	resp.About = CollectAboutData(count, dbparams.Offset, dbparams.Limit, r)
	WriteJsonResponse(w, resp)
}

func (a *ApiHandler) PostPeers(w http.ResponseWriter, r *http.Request) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: map[string]string{"method": "PostPeers"},
	})
	var newPeer oapi.Peer
	err := json.NewDecoder(r.Body).Decode(&newPeer)
	if err != nil {
		AddBadRequestError(ctx, w, err)
		return
	}
	_, err = a.illRepo.GetPeerById(ctx, newPeer.Id)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		AddInternalError(ctx, w, err)
		return
	} else if err == nil {
		AddBadRequestError(ctx, w, fmt.Errorf("ID %v is already used", newPeer.Id))
		return
	}
	for _, s := range newPeer.Symbols {
		if !strings.Contains(s, ":") {
			AddBadRequestError(ctx, w, fmt.Errorf("symbol should be in \"ISIL:SYMBOL\" format but got %v", s))
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
		AddInternalError(ctx, w, err)
		return
	}
	apiPeer := toApiPeer(peer, symbols, branchSymbols)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(apiPeer)
}

func (a *ApiHandler) DeletePeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
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
			AddNotFoundError(w)
			return
		} else {
			AddInternalError(ctx, w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *ApiHandler) GetPeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPeersSymbol", "id": id},
	})
	peer, err := a.illRepo.GetPeerById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			AddNotFoundError(w)
			return
		} else {
			AddInternalError(ctx, w, err)
			return
		}
	}
	symbols, err := a.illRepo.GetSymbolsByPeerId(ctx, peer.ID)
	if err != nil {
		AddInternalError(ctx, w, err)
		return
	}
	branchSymbols, err := a.illRepo.GetBranchSymbolsByPeerId(ctx, peer.ID)
	if err != nil {
		AddInternalError(ctx, w, err)
		return
	}
	apiPeer := toApiPeer(peer, symbols, branchSymbols)
	WriteJsonResponse(w, apiPeer)
}

func (a *ApiHandler) PutPeersId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: map[string]string{"method": "PutPeersSymbol", "id": id},
	})
	peer, err := a.illRepo.GetPeerById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			AddNotFoundError(w)
			return
		} else {
			AddInternalError(ctx, w, err)
			return
		}
	}
	var update oapi.Peer
	err = json.NewDecoder(r.Body).Decode(&update)
	if err != nil {
		AddBadRequestError(ctx, w, err)
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
		bytes, err := json.Marshal(update.CustomData)
		if err != nil {
			AddInternalError(ctx, w, err)
			return
		}
		err = json.Unmarshal(bytes, &peer.CustomData)
		if err != nil {
			AddInternalError(ctx, w, err)
			return
		}
	} else {
		peer.CustomData = directory.Entry{}
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
		AddInternalError(ctx, w, err)
		return
	}
	apiPeer := toApiPeer(peer, symbols, branchSymbols)
	WriteJsonResponse(w, apiPeer)
}

func (a *ApiHandler) GetLocatedSuppliers(w http.ResponseWriter, r *http.Request, params oapi.GetLocatedSuppliersParams) {
	logParams := map[string]string{"method": "GetLocatedSuppliers"}
	if params.IllTransactionId != nil {
		logParams["IllTransactionId"] = *params.IllTransactionId
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
		Other: logParams,
	})
	tran, err := a.getIllTranFromParams(ctx, w, r, params.RequesterSymbol,
		params.RequesterReqId, params.IllTransactionId)
	if err != nil {
		return
	}
	var resp oapi.LocatedSuppliers
	resp.Items = make([]oapi.LocatedSupplier, 0)
	if tran == nil {
		WriteJsonResponse(w, resp)
		return
	}
	var supList []ill_db.LocatedSupplier
	var count int64
	supList, count, err = a.illRepo.GetLocatedSuppliersByIllTransaction(ctx, tran.ID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { //DB error
		AddInternalError(ctx, w, err)
		return
	}
	resp.About.Count = count
	for _, supplier := range supList {
		resp.Items = append(resp.Items, toApiLocatedSupplier(r, supplier))
	}
	WriteJsonResponse(w, resp)
}

func (a *ApiHandler) PostArchiveIllTransactions(w http.ResponseWriter, r *http.Request, params oapi.PostArchiveIllTransactionsParams) {
	logParams := map[string]string{"method": "PostArchiveIllTransactions", "ArchiveDelay": params.ArchiveDelay, "ArchiveStatus": params.ArchiveStatus}
	// a background process so use background context instead of request context to avoid cancellation when request is finished
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: logParams,
	})
	err := service.Archive(ctx, a.illRepo, params.ArchiveStatus, params.ArchiveDelay, true)
	if err != nil {
		AddBadRequestError(ctx, w, err)
		return
	}
	WriteJsonResponse(w, oapi.StatusMessage{
		Status: ARCHIVE_PROCESS_STARTED,
	})
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

func ToApiEvent(event events.Event, illId string, prId *string) oapi.Event {
	api := oapi.Event{
		Id:               event.ID,
		Timestamp:        event.Timestamp.Time,
		IllTransactionID: illId,
		EventType:        string(event.EventType),
		EventName:        string(event.EventName),
		EventStatus:      string(event.EventStatus),
		ParentID:         toString(event.ParentID),
		PatronRequestID:  prId,
	}
	api.EventData = &event.EventData
	api.ResultData = &event.ResultData
	return api
}

func toApiLocatedSupplier(r *http.Request, sup ill_db.LocatedSupplier) oapi.LocatedSupplier {
	return oapi.LocatedSupplier{
		Id:                sup.ID,
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
		SupplierPeerLink:  Link(r, Path(PEERS_PATH, sup.SupplierID), nil),
	}
}

func toApiIllTransaction(r *http.Request, trans ill_db.IllTransaction) oapi.IllTransaction {
	api := oapi.IllTransaction{
		Id:        trans.ID,
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
	api.EventsLink = Link(r, Path(EVENTS_PATH), Query("ill_transaction_id", trans.ID))
	api.LocatedSuppliersLink = Link(r, Path(LOCATED_SUPPLIERS_PATH), Query("ill_transaction_id", trans.ID))
	if trans.RequesterID.Valid {
		api.RequesterPeerLink = Link(r, Path(PEERS_PATH, trans.RequesterID.String), nil)
	}
	api.IllTransactionData = trans.IllTransactionData
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

	customData := peer.CustomData

	return oapi.Peer{
		Id:            peer.ID,
		Symbols:       list,
		Name:          peer.Name,
		Url:           peer.Url,
		RefreshPolicy: toApiPeerRefreshPolicy(peer.RefreshPolicy),
		Vendor:        peer.Vendor,
		RefreshTime:   &peer.RefreshTime.Time,
		LoansCount:    &peer.LoansCount,
		BorrowsCount:  &peer.BorrowsCount,
		CustomData:    &customData,
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
	var entry directory.Entry
	if peer.CustomData != nil {
		entry = *peer.CustomData
	}
	httpHeaders := make(map[string]string)
	if peer.HttpHeaders != nil {
		httpHeaders = *peer.HttpHeaders
	}
	db := ill_db.Peer{
		ID:            peer.Id,
		Name:          peer.Name,
		Url:           peer.Url,
		Vendor:        peer.Vendor,
		BrokerMode:    string(peer.BrokerMode),
		RefreshPolicy: toDbRefreshPolicy(peer.RefreshPolicy),
		RefreshTime: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		CustomData:  entry,
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
