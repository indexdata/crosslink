package prapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var waitingReqs = map[string]RequestWait{}

type PatronRequestApiHandler struct {
	limitDefault         int32
	prRepo               pr_db.PrRepo
	eventBus             events.EventBus
	actionMappingService prservice.ActionMappingService
	tenant               common.Tenant
}

func NewPrApiHandler(prRepo pr_db.PrRepo, eventBus events.EventBus, tenant common.Tenant, limitDefault int32) PatronRequestApiHandler {
	return PatronRequestApiHandler{
		limitDefault:         limitDefault,
		prRepo:               prRepo,
		eventBus:             eventBus,
		actionMappingService: prservice.ActionMappingService{},
		tenant:               tenant,
	}
}

func (a *PatronRequestApiHandler) GetPatronRequests(w http.ResponseWriter, r *http.Request, params proapi.GetPatronRequestsParams) {
	symbol, err := api.GetSymbolForRequest(r, a.tenant, params.XOkapiTenant, params.Symbol)
	logParams := map[string]string{"method": "GetPatronRequests", "side": params.Side, "symbol": symbol}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: logParams,
	})
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	limit := a.limitDefault
	if params.Limit != nil {
		limit = *params.Limit
	}
	var offset int32 = 0
	if params.Offset != nil {
		offset = *params.Offset
	}
	var qb *cqlbuilder.QueryBuilder
	if params.Cql == nil {
		qb = cqlbuilder.NewQuery()
		qb.Search("cql.allRecords").Term("1")
	} else {
		qb, err = cqlbuilder.NewQueryFromString(*params.Cql)
		if err != nil {
			addBadRequestError(ctx, w, err)
			return
		}
	}
	var side pr_db.PatronRequestSide
	if isSideParamValid(params.Side) {
		side = pr_db.PatronRequestSide(params.Side)
		_, err = qb.And().Search("side").Term(params.Side).Build()
		if err != nil {
			addBadRequestError(ctx, w, err)
			return
		}
	}
	qb, err = addOwnerRestriction(qb, symbol, side)
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	cql, err := qb.Build()
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	cqlStr := cql.String()
	prs, count, err := a.prRepo.ListPatronRequests(ctx, pr_db.ListPatronRequestsParams{Limit: limit, Offset: offset}, &cqlStr)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { //DB error
		addInternalError(ctx, w, err)
		return
	}
	var responseItems []proapi.PatronRequest
	for _, pr := range prs {
		var illRequest iso18626.Request
		if pr.IllRequest != nil {
			err = json.Unmarshal(pr.IllRequest, &illRequest)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				addInternalError(ctx, w, err)
				return
			}
		} else {
			illRequest = iso18626.Request{}
		}
		responseItems = append(responseItems, toApiPatronRequest(pr, illRequest))
	}

	resp := proapi.PatronRequests{Items: responseItems}
	resp.About = proapi.About(api.CollectAboutData(count, offset, limit, r))
	writeJsonResponse(w, resp)
}

func addOwnerRestriction(queryBuilder *cqlbuilder.QueryBuilder, symbol string, side pr_db.PatronRequestSide) (*cqlbuilder.QueryBuilder, error) {
	var err error
	switch side {
	case prservice.SideLending:
		_, err = queryBuilder.And().
			Search("side").Term(string(prservice.SideLending)).
			And().Search("supplier_symbol").Term(symbol).
			Build()
	case prservice.SideBorrowing:
		_, err = queryBuilder.And().
			Search("side").Term(string(prservice.SideBorrowing)).
			And().Search("requester_symbol").Term(symbol).
			Build()
	default:
		_, err = queryBuilder.And().
			BeginClause().
			Search("side").Term(string(prservice.SideLending)).
			And().Search("supplier_symbol").Term(symbol).
			EndClause().
			Or().
			BeginClause().Search("side").Term(string(prservice.SideBorrowing)).
			And().Search("requester_symbol").Term(symbol).
			EndClause().
			Build()
	}
	return queryBuilder, err
}

func (a *PatronRequestApiHandler) PostPatronRequests(w http.ResponseWriter, r *http.Request, params proapi.PostPatronRequestsParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "PostPatronRequests"},
	})
	var newPr proapi.CreatePatronRequest
	err := json.NewDecoder(r.Body).Decode(&newPr)
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	symbol, err := api.GetSymbolForRequest(r, a.tenant, params.XOkapiTenant, newPr.RequesterSymbol)
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	newPr.RequesterSymbol = &symbol
	dbreq, err := a.toDbPatronRequest(ctx, newPr, params.XOkapiTenant)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	pr, err := a.prRepo.SavePatronRequest(ctx, (pr_db.SavePatronRequestParams)(dbreq))
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	//TODO this starts an async action and we need to publish the outcome back to the client
	// we have two options:
	// 1) block like we do in the POST /patron-requests/{id}/action endpoint
	// 2) return 202 Accepted and send the update via the /sse/events endpoint
	// Option 2 requires that we define SSE events for patron request updates
	action := prservice.BorrowerActionValidate
	_, err = a.eventBus.CreateTask(pr.ID, events.EventNameInvokeAction, events.EventData{CommonEventData: events.CommonEventData{Action: &action}}, events.EventDomainPatronRequest, nil)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	var illRequest iso18626.Request
	err = json.Unmarshal(pr.IllRequest, &illRequest)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		addInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiPatronRequest(pr, illRequest))
}

func (a *PatronRequestApiHandler) DeletePatronRequestsId(w http.ResponseWriter, r *http.Request, id string, params proapi.DeletePatronRequestsIdParams) {
	symbol, err := api.GetSymbolForRequest(r, a.tenant, params.XOkapiTenant, params.Symbol)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "DeletePatronRequestsId", "id": id, "side": params.Side, "symbol": symbol},
	})
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	pr, err := a.getPatronRequestById(ctx, id, params.Side, symbol)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	if pr == nil {
		addNotFoundError(w)
		return
	}
	err = a.prRepo.WithTxFunc(ctx, func(repo pr_db.PrRepo) error {
		return repo.DeletePatronRequest(ctx, pr.ID)
	})
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *PatronRequestApiHandler) getPatronRequestById(ctx common.ExtendedContext, id string, side string, symbol string) (*pr_db.PatronRequest, error) {
	pr, err := a.prRepo.GetPatronRequestById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if isOwner(pr, symbol) && (!isSideParamValid(side) || string(pr.Side) == side) {
		return &pr, nil
	}
	return nil, nil
}

func isSideParamValid(side string) bool {
	return side == string(prservice.SideBorrowing) || side == string(prservice.SideLending)
}

func isOwner(pr pr_db.PatronRequest, symbol string) bool {
	return (string(pr.Side) == string(prservice.SideBorrowing) && pr.RequesterSymbol.String == symbol) ||
		(string(pr.Side) == string(prservice.SideLending) && pr.SupplierSymbol.String == symbol)
}

func (a *PatronRequestApiHandler) GetPatronRequestsId(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdParams) {
	symbol, err := api.GetSymbolForRequest(r, a.tenant, params.XOkapiTenant, params.Symbol)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPatronRequestsId", "id": id, "side": params.Side, "symbol": symbol},
	})
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	pr, err := a.getPatronRequestById(ctx, id, params.Side, symbol)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	if pr == nil {
		addNotFoundError(w)
		return
	}
	var illRequest iso18626.Request
	err = json.Unmarshal(pr.IllRequest, &illRequest)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, toApiPatronRequest(*pr, illRequest))
}

func (a *PatronRequestApiHandler) GetPatronRequestsIdActions(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdActionsParams) {
	symbol, err := api.GetSymbolForRequest(r, a.tenant, params.XOkapiTenant, params.Symbol)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPatronRequestsIdActions", "id": id, "side": params.Side, "symbol": symbol},
	})
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	pr, err := a.getPatronRequestById(ctx, id, params.Side, symbol)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	if pr == nil {
		addNotFoundError(w)
		return
	}
	actions := a.actionMappingService.GetActionMapping(*pr).GetActionsForPatronRequest(*pr)
	writeJsonResponse(w, actions)
}

func (a *PatronRequestApiHandler) PostPatronRequestsIdAction(w http.ResponseWriter, r *http.Request, id string, params proapi.PostPatronRequestsIdActionParams) {
	symbol, err := api.GetSymbolForRequest(r, a.tenant, params.XOkapiTenant, params.Symbol)
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "PostPatronRequestsIdAction", "id": id, "side": params.Side, "symbol": symbol},
	})
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	pr, err := a.getPatronRequestById(ctx, id, params.Side, symbol)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	if pr == nil {
		addNotFoundError(w)
		return
	}
	var action proapi.ExecuteAction
	err = json.NewDecoder(r.Body).Decode(&action)
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}

	if !a.actionMappingService.GetActionMapping(*pr).IsActionAvailable(*pr, pr_db.PatronRequestAction(action.Action)) {
		addBadRequestError(ctx, w, errors.New("Action "+action.Action+" is not allowed for patron request "+id))
		return
	}
	eventAction := pr_db.PatronRequestAction(action.Action)
	data := events.EventData{CommonEventData: events.CommonEventData{Action: &eventAction}}
	if action.ActionParams != nil {
		data.CustomData = *action.ActionParams
	}
	eventId, err := a.eventBus.CreateTaskBroadcast(pr.ID, events.EventNameInvokeAction, data, events.EventDomainPatronRequest, nil)
	if err != nil {
		addInternalError(ctx, w, err)
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

func (a *PatronRequestApiHandler) ConfirmActionProcess(ctx common.ExtendedContext, event events.Event) {
	if waitingRequest, ok := waitingReqs[event.ID]; ok {
		result := proapi.ActionResult{
			ActionResult: string(event.EventStatus),
		}
		if event.ResultData.Note != "" {
			result.Message = &event.ResultData.Note
		}
		writeJsonResponse(*waitingRequest.w, result)
		waitingRequest.wg.Done()
	}
}

func writeJsonResponse(w http.ResponseWriter, resp any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

func addInternalError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
	errorString := err.Error()
	resp := proapi.Error{
		Error: &errorString,
	}
	ctx.Logger().Error("error serving api request", "error", err.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(resp)
}

func addBadRequestError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
	errorString := err.Error()
	resp := proapi.Error{
		Error: &errorString,
	}
	ctx.Logger().Error("error serving api request", "error", err.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(resp)
}

func addNotFoundError(w http.ResponseWriter) {
	errorString := "not found"
	resp := proapi.Error{
		Error: &errorString,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(resp)
}

func toApiPatronRequest(request pr_db.PatronRequest, illRequest iso18626.Request) proapi.PatronRequest {
	return proapi.PatronRequest{
		Id:                 request.ID,
		Timestamp:          request.Timestamp.Time,
		State:              string(request.State),
		Side:               string(request.Side),
		Patron:             toString(request.Patron),
		RequesterSymbol:    toString(request.RequesterSymbol),
		SupplierSymbol:     toString(request.SupplierSymbol),
		IllRequest:         utils.Must(common.StructToMap(illRequest)),
		RequesterRequestId: toString(request.RequesterReqID),
	}
}

func toString(text pgtype.Text) *string {
	var value *string
	if text.Valid {
		value = &text.String
	}
	return value
}

func (a *PatronRequestApiHandler) toDbPatronRequest(ctx common.ExtendedContext, request proapi.CreatePatronRequest, tenant *string) (pr_db.PatronRequest, error) {
	creationTime := pgtype.Timestamp{Valid: true, Time: time.Now()}
	var id string
	if request.Id != nil {
		id = *request.Id
	} else {
		prefix := strings.SplitN(*request.RequesterSymbol, ":", 2)[1]
		hrid, err := a.prRepo.GetNextHrid(ctx, prefix)
		if err != nil {
			return pr_db.PatronRequest{}, err
		}
		id = hrid
	}
	var illRequest []byte
	if request.IllRequest != nil {
		illRequest = utils.Must(json.Marshal(request.IllRequest))
		var isoRequest iso18626.Request
		err := json.Unmarshal(illRequest, &isoRequest)
		if err != nil {
			return pr_db.PatronRequest{}, err
		}
		isoRequest.Header.Timestamp = utils.XSDDateTime{Time: creationTime.Time}
		isoRequest.Header.RequestingAgencyRequestId = id
		illRequest = utils.Must(json.Marshal(isoRequest))
	}

	return pr_db.PatronRequest{
		ID:              id,
		Timestamp:       creationTime,
		State:           prservice.BorrowerStateNew,
		Side:            prservice.SideBorrowing,
		Patron:          getDbText(request.Patron),
		RequesterSymbol: getDbText(request.RequesterSymbol),
		SupplierSymbol:  getDbText(request.SupplierSymbol),
		IllRequest:      illRequest,
		Tenant:          getDbText(tenant),
	}, nil
}

func getId(id string) string {
	if id == "" {
		return uuid.NewString()
	}
	return id
}

func getDbText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{
		Valid:  true,
		String: *value,
	}
}

type RequestWait struct {
	w  *http.ResponseWriter
	wg *sync.WaitGroup
}
