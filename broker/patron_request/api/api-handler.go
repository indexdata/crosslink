package prapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/indexdata/cql-go/cqlbuilder"
	"github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/handler"
	"github.com/indexdata/crosslink/broker/oapi"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/patron_request/proapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type ActionTaskProcessor interface {
	ProcessInvokeActionTask(ctx common.ExtendedContext, event events.Event) (events.Event, error)
}

var illRequestValidator = validator.New(validator.WithRequiredStructEnabled())
var brokerSymbol = utils.GetEnv("BROKER_SYMBOL", "ISIL:BROKER")
var errInvalidPatronRequest = errors.New("invalid patron request")

type PatronRequestApiHandler struct {
	limitDefault         int32
	prRepo               pr_db.PrRepo
	eventBus             events.EventBus
	eventRepo            events.EventRepo
	actionMappingService prservice.ActionMappingService
	autoActionRunner     prservice.AutoActionRunner
	actionTaskProcessor  ActionTaskProcessor
	tenantResolver       tenant.TenantResolver
	notificationSender   prservice.PatronRequestNotificationService
}

func NewPrApiHandler(prRepo pr_db.PrRepo, eventBus events.EventBus,
	eventRepo events.EventRepo, tenantResolver tenant.TenantResolver, iso18626Handler handler.Iso18626HandlerInterface, limitDefault int32) PatronRequestApiHandler {
	return PatronRequestApiHandler{
		limitDefault:         limitDefault,
		prRepo:               prRepo,
		eventBus:             eventBus,
		eventRepo:            eventRepo,
		actionMappingService: prservice.ActionMappingService{SMService: &prservice.StateModelService{}},
		tenantResolver:       tenantResolver,
		notificationSender:   *prservice.CreatePatronRequestNotificationService(prRepo, eventBus, iso18626Handler),
	}
}

func (a *PatronRequestApiHandler) SetAutoActionRunner(autoActionRunner prservice.AutoActionRunner) {
	a.autoActionRunner = autoActionRunner
}

func (a *PatronRequestApiHandler) SetActionTaskProcessor(actionTaskProcessor ActionTaskProcessor) {
	a.actionTaskProcessor = actionTaskProcessor
}

func decodeRequiredBody[T any](r *http.Request, dst *T) error {
	if r.Body == nil || r.Body == http.NoBody {
		return errors.New("body is required")
	}
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("body is required")
		}
		return err
	}
	return nil
}

func (a *PatronRequestApiHandler) getRequestSymbol(ctx common.ExtendedContext, r *http.Request, requestedSymbol *string) (string, error) {
	tenant, err := a.tenantResolver.Resolve(ctx, r, requestedSymbol)
	if err != nil {
		return "", err
	}
	return tenant.GetRequestSymbol()
}

func (a *PatronRequestApiHandler) GetStateModelModelsModel(w http.ResponseWriter, r *http.Request, model string, params proapi.GetStateModelModelsModelParams) {
	stateModel, err := a.actionMappingService.GetStateModel(model)
	if err != nil {
		ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{
			Other: map[string]string{"method": "GetStateModelModelsModel", "model": model},
		})
		api.AddInternalError(ctx, w, err)
		return
	}
	if stateModel == nil {
		api.AddNotFoundError(w)
		return
	}
	api.WriteJsonResponse(w, *stateModel)
}

func (a *PatronRequestApiHandler) GetStateModelCapabilities(w http.ResponseWriter, r *http.Request, params proapi.GetStateModelCapabilitiesParams) {
	api.WriteJsonResponse(w, prservice.BuiltInStateModelCapabilities())
}

func (a *PatronRequestApiHandler) GetPatronRequests(w http.ResponseWriter, r *http.Request, params proapi.GetPatronRequestsParams) {
	logParams := map[string]string{"method": "GetPatronRequests"}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	symbol, err := a.getRequestSymbol(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

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
			api.AddBadRequestError(ctx, w, err)
			return
		}
	}
	var side pr_db.PatronRequestSide
	if isSideParamValid(params.Side) {
		side = pr_db.PatronRequestSide(*params.Side)
		_, err = qb.And().Search("side").Term(*params.Side).Build()
		if err != nil {
			api.AddBadRequestError(ctx, w, err)
			return
		}
	}
	if params.RequesterReqId != nil {
		_, err = qb.And().Search("requester_req_id_exact").Term(*params.RequesterReqId).Build()
		if err != nil {
			api.AddBadRequestError(ctx, w, err)
			return
		}
	}
	if symbol != "" {
		qb, err = AddOwnerRestriction(qb, symbol, side)
		if err != nil {
			api.AddBadRequestError(ctx, w, err)
			return
		}
	}
	cql, err := qb.Build()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	cqlStr := cql.String()
	prs, count, err := a.prRepo.ListPatronRequestsSearchView(ctx, pr_db.ListPatronRequestsParams{Limit: limit, Offset: offset}, &cqlStr)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { //DB error
		api.AddInternalError(ctx, w, err)
		return
	}
	var responseItems []proapi.PatronRequest
	for _, pr := range prs {
		responseItems = append(responseItems, toApiPatronRequest(r, pr))
	}

	resp := proapi.PatronRequests{Items: responseItems}
	resp.About = proapi.About(api.CollectAboutData(count, offset, limit, r))
	api.WriteJsonResponse(w, resp)
}

func AddOwnerRestriction(queryBuilder *cqlbuilder.QueryBuilder, symbol string, side pr_db.PatronRequestSide) (*cqlbuilder.QueryBuilder, error) {
	var err error
	switch side {
	case prservice.SideLending:
		_, err = queryBuilder.And().
			Search("side").Term(string(prservice.SideLending)).
			And().Search("supplier_symbol_exact").Term(symbol).
			Build()
	case prservice.SideBorrowing:
		_, err = queryBuilder.And().
			Search("side").Term(string(prservice.SideBorrowing)).
			And().Search("requester_symbol_exact").Term(symbol).
			Build()
	default:
		_, err = queryBuilder.And().
			BeginClause().
			Search("side").Term(string(prservice.SideLending)).
			And().Search("supplier_symbol_exact").Term(symbol).
			Or().
			BeginClause().Search("side").Term(string(prservice.SideBorrowing)).
			And().Search("requester_symbol_exact").Term(symbol).
			EndClause().
			EndClause().
			Build()
	}
	return queryBuilder, err
}

func (a *PatronRequestApiHandler) PostPatronRequests(w http.ResponseWriter, r *http.Request, params proapi.PostPatronRequestsParams) {
	logParams := map[string]string{"method": "PostPatronRequests"}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	var newPr proapi.CreatePatronRequest
	err := decodeRequiredBody(r, &newPr)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	tenant, err := a.tenantResolver.Resolve(ctx, r, newPr.RequesterSymbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	if symbol == "" {
		api.AddBadRequestError(ctx, w, errors.New("symbol must be specified"))
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	newPr.RequesterSymbol = &symbol
	creationTime := pgtype.Timestamp{Valid: true, Time: time.Now()}
	illRequest, requesterReqId, err := a.parseAndValidateIllRequest(ctx, &newPr, creationTime.Time)
	if err != nil {
		if errors.Is(err, errInvalidPatronRequest) {
			api.AddBadRequestError(ctx, w, err)
			return
		}
		api.AddInternalError(ctx, w, err)
		return
	}
	dbreq := buildDbPatronRequest(&newPr, params.XOkapiTenant, creationTime, requesterReqId, illRequest)
	pr, err := a.prRepo.CreatePatronRequest(ctx, pr_db.CreatePatronRequestParams(dbreq))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgerrcode.IsIntegrityConstraintViolation(pgErr.Code) {
			api.AddBadRequestError(ctx, w, errors.New("a patron request with this ID already exists"))
			return
		}
		api.AddInternalError(ctx, w, err)
		return
	}
	if a.autoActionRunner != nil {
		err = a.autoActionRunner.RunAutoActionsOnStateEntry(ctx, pr, nil, tenant.GetUser())
		if err != nil {
			api.AddInternalError(ctx, w, err)
			return
		}
	}
	prView, err := a.prRepo.GetPatronRequestSearchView(ctx, pr.ID)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Location", api.Link(r, api.Path("patron_requests", pr.ID), nil))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiPatronRequest(r, prView))
}

func (a *PatronRequestApiHandler) DeletePatronRequestsId(w http.ResponseWriter, r *http.Request, id string, params proapi.DeletePatronRequestsIdParams) {
	logParams := map[string]string{"method": "DeletePatronRequestsId", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}
	err = a.prRepo.WithTxFunc(ctx, func(repo pr_db.PrRepo) error {
		return repo.DeletePatronRequest(ctx, pr.ID)
	})
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func getOwnerSymbol(side pr_db.PatronRequestSide, requesterSymbol pgtype.Text, supplierSymbol pgtype.Text) string {
	if side == prservice.SideBorrowing && requesterSymbol.Valid {
		return requesterSymbol.String
	}
	if side == prservice.SideLending && supplierSymbol.Valid {
		return supplierSymbol.String
	}
	return ""
}

func (a *PatronRequestApiHandler) getOwnedPatronRequest(w http.ResponseWriter, ctx common.ExtendedContext, id string, side *string, tenant tenant.Tenant) *pr_db.PatronRequest {
	pr, err := a.prRepo.GetPatronRequestById(ctx, id)
	if err != nil {
		handleDbError(w, ctx, err)
		return nil
	}
	if !a.checkOwnership(w, ctx, pr.Side, pr.RequesterSymbol, pr.SupplierSymbol, side, tenant) {
		return nil
	}
	return &pr
}

func (a *PatronRequestApiHandler) getOwnedPatronRequestSearchView(w http.ResponseWriter, ctx common.ExtendedContext, id string, side *string, tenant tenant.Tenant) *pr_db.PatronRequestSearchView {
	pr, err := a.prRepo.GetPatronRequestSearchView(ctx, id)
	if err != nil {
		handleDbError(w, ctx, err)
		return nil
	}
	if !a.checkOwnership(w, ctx, pr.Side, pr.RequesterSymbol, pr.SupplierSymbol, side, tenant) {
		return nil
	}
	return &pr
}

func (a *PatronRequestApiHandler) checkOwnership(w http.ResponseWriter, ctx common.ExtendedContext, prSide pr_db.PatronRequestSide, requesterSymbol pgtype.Text, supplierSymbol pgtype.Text, side *string, tenant tenant.Tenant) bool {
	isOwner, err := tenant.IsOwnerOf(getOwnerSymbol(prSide, requesterSymbol, supplierSymbol))
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return false
	}
	if isOwner && (!isSideParamValid(side) || string(prSide) == *side) {
		return true
	}
	api.AddNotFoundError(w)
	return false
}

func handleDbError(w http.ResponseWriter, ctx common.ExtendedContext, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		api.AddNotFoundError(w)
		return
	}
	api.AddInternalError(ctx, w, err)
}

func isSideParamValid(side *string) bool {
	return side != nil && (*side == string(prservice.SideBorrowing) || *side == string(prservice.SideLending))
}

func (a *PatronRequestApiHandler) GetPatronRequestsId(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdParams) {
	logParams := map[string]string{"method": "GetPatronRequestsId", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	pr := a.getOwnedPatronRequestSearchView(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}
	api.WriteJsonResponse(w, toApiPatronRequest(r, *pr))
}

func (a *PatronRequestApiHandler) GetPatronRequestsIdActions(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdActionsParams) {
	logParams := map[string]string{"method": "GetPatronRequestsIdActions", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}
	actionMapping, err := a.actionMappingService.GetActionMapping(*pr)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	actions := actionMapping.GetAllowedActionsForPatronRequest(*pr)
	api.WriteJsonResponse(w, actions)
}

func (a *PatronRequestApiHandler) PostPatronRequestsIdAction(w http.ResponseWriter, r *http.Request, id string, params proapi.PostPatronRequestsIdActionParams) {
	logParams := map[string]string{"method": "PostPatronRequestsIdAction", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}
	var action proapi.ExecuteAction
	err = decodeRequiredBody(r, &action)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}

	actionMapping, err := a.actionMappingService.GetActionMapping(*pr)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	fromState := string(pr.State)
	if !actionMapping.IsActionAvailable(*pr, pr_db.PatronRequestAction(action.Action)) {
		api.AddBadRequestError(ctx, w, errors.New("Action "+action.Action+" is not allowed for patron request "+id+" in state "+string(pr.State)))
		return
	}
	eventAction := pr_db.PatronRequestAction(action.Action)
	data := events.EventData{CommonEventData: events.CommonEventData{
		Action: &eventAction,
		User:   tenant.GetUser(),
	}}
	if action.ActionParams != nil {
		data.CustomData = *action.ActionParams
	}
	if a.actionTaskProcessor == nil {
		api.AddInternalError(ctx, w, errors.New("action task processor not configured"))
		return
	}
	eventId, err := a.eventBus.CreateTask(pr.ID, events.EventNameInvokeAction, data, events.EventDomainPatronRequest, nil, events.SignalConsumers)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	completedEvent, err := a.actionTaskProcessor.ProcessInvokeActionTask(ctx, events.Event{
		ID:              eventId,
		PatronRequestID: pr.ID,
		EventData:       data,
	})
	// Manual invoke-action requests are handled inline and return the completed task result.
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	var message *string
	if completedEvent.ResultData.EventError != nil {
		message = &completedEvent.ResultData.EventError.Message
	}
	outcome := completedEvent.ResultData.ActionResult.Outcome
	result := proapi.ActionResult{
		Result:    string(completedEvent.EventStatus),
		Message:   message,
		Outcome:   outcome,
		FromState: fromState,
		ToState:   completedEvent.ResultData.ActionResult.ToState,
	}
	if completedEvent.ResultData.Note != "" {
		result.Message = &completedEvent.ResultData.Note
	}
	api.WriteJsonResponse(w, result)
}

func (a *PatronRequestApiHandler) GetPatronRequestsIdEvents(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdEventsParams) {
	logParams := map[string]string{"method": "GetPatronRequestsIdEvents", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}
	eventsList, err := a.eventRepo.GetPatronRequestEvents(ctx, pr.ID)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}

	responseItems := make([]oapi.Event, 0, len(eventsList))
	for _, event := range eventsList {
		responseItems = append(responseItems, api.ToApiEvent(event, "", &event.PatronRequestID))
	}
	resp := oapi.Events{Items: responseItems}
	resp.About.Count = int64(len(responseItems))
	api.WriteJsonResponse(w, resp)
}

func (a *PatronRequestApiHandler) GetPatronRequestsIdItems(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdItemsParams) {
	logParams := map[string]string{"method": "GetPatronRequestsIdItems", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}
	itemsList, err := a.prRepo.GetItemsByPrId(ctx, pr.ID)
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}

	responseItems := make([]proapi.PrItem, 0, len(itemsList))
	for _, item := range itemsList {
		responseItems = append(responseItems, toApiItem(item))
	}
	resp := proapi.PrItems{Items: responseItems}
	resp.About.Count = int64(len(responseItems))
	api.WriteJsonResponse(w, resp)
}

func (a *PatronRequestApiHandler) GetPatronRequestsIdNotifications(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdNotificationsParams) {
	logParams := map[string]string{"method": "GetPatronRequestsIdNotifications", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	limit := a.limitDefault
	if params.Limit != nil {
		limit = *params.Limit
	}
	var offset int32 = 0
	if params.Offset != nil {
		offset = *params.Offset
	}

	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}
	kind := ""
	if params.Kind != nil {
		kind = string(*params.Kind)
	}
	list, fullCount, err := a.prRepo.GetNotificationsByPrId(ctx, pr_db.GetNotificationsByPrIdParams{PrID: pr.ID, Limit: limit, Offset: offset, Kind: kind})
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}

	responseList := make([]proapi.PrNotification, 0, len(list))
	for _, n := range list {
		apiN, inErr := toApiNotification(n)
		if inErr != nil {
			api.AddInternalError(ctx, w, inErr)
			return
		}
		responseList = append(responseList, apiN)
	}
	resp := proapi.PrNotifications{Items: responseList}
	resp.About = proapi.About(api.CollectAboutData(fullCount, offset, limit, r))
	api.WriteJsonResponse(w, resp)
}

func (a *PatronRequestApiHandler) PostPatronRequestsIdNotifications(w http.ResponseWriter, r *http.Request, id string, params proapi.PostPatronRequestsIdNotificationsParams) {
	logParams := map[string]string{"method": "PostPatronRequestsIdNotifications", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	var newNotification proapi.CreatePrNotification
	err = decodeRequiredBody(r, &newNotification)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	if strings.TrimSpace(newNotification.Note) == "" {
		api.AddBadRequestError(ctx, w, errors.New("note is required"))
		return
	}

	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}

	dbNotification := toDbNotification(newNotification, *pr)
	dbNotification, err = a.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams(dbNotification))
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	apiN, inErr := toApiNotification(dbNotification)
	if inErr != nil {
		api.AddInternalError(ctx, w, inErr)
		return
	}

	err = a.notificationSender.SendPatronRequestNotification(ctx, *pr, dbNotification)
	if err != nil {
		ctx.Logger().Error("failed to send notification for patron request", "notificationId", dbNotification.ID, "error", err.Error())
	}

	//w.Header().Set("Location", api.Link(r, api.Path("patron_requests", id, "notifications", dbNotification.ID), nil))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(apiN)
}

func (a *PatronRequestApiHandler) PutPatronRequestsIdNotificationsNotificationIdReceipt(w http.ResponseWriter, r *http.Request, id string, notificationId string, params proapi.PutPatronRequestsIdNotificationsNotificationIdReceiptParams) {
	logParams := map[string]string{"method": "PutPatronRequestsIdNotificationsNotificationIdReceipt", "id": id}
	if params.Side != nil {
		logParams["side"] = *params.Side
	}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	tenant, err := a.tenantResolver.Resolve(ctx, r, params.Symbol)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	symbol, err := tenant.GetRequestSymbol()
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}
	logParams["symbol"] = symbol
	ctx = common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})
	var receipt proapi.UpdateNotificationReceipt
	err = decodeRequiredBody(r, &receipt)
	if err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}

	if err = validator.New().Struct(receipt); err != nil {
		api.AddBadRequestError(ctx, w, err)
		return
	}

	pr := a.getOwnedPatronRequest(w, ctx, id, params.Side, tenant)
	if pr == nil {
		return
	}

	notification, err := a.prRepo.GetNotificationById(ctx, notificationId)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			api.AddNotFoundError(w)
			return
		}
		api.AddInternalError(ctx, w, err)
		return
	}

	if notification.PrID != pr.ID {
		api.AddNotFoundError(w)
		return
	}

	notification.Receipt = pr_db.NotificationReceipt(receipt.Receipt)
	if !notification.AcknowledgedAt.Valid {
		notification.AcknowledgedAt = pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		}
	}
	notification, err = a.prRepo.SaveNotification(ctx, pr_db.SaveNotificationParams(notification))
	if err != nil {
		api.AddInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}

func toApiPatronRequest(r *http.Request, request pr_db.PatronRequestSearchView) proapi.PatronRequest {
	items := []proapi.PrItem{}
	for _, item := range request.Items {
		items = append(items, toApiPrItem(item))
	}
	ownerSymbol := getOwnerSymbol(request.Side, request.RequesterSymbol, request.SupplierSymbol)
	var notificationsLink *string
	var itemsLink *string
	var availableActionsLink *string
	var eventsLink *string
	if ownerSymbol != "" {
		linkQuery := api.Query("symbol", ownerSymbol)
		notificationsLinkValue := api.Link(r, api.Path("patron_requests", request.ID, "notifications"), linkQuery)
		notificationsLink = &notificationsLinkValue
		itemsLinkValue := api.Link(r, api.Path("patron_requests", request.ID, "items"), linkQuery)
		itemsLink = &itemsLinkValue
		availableActionsLinkValue := api.Link(r, api.Path("patron_requests", request.ID, "actions"), linkQuery)
		availableActionsLink = &availableActionsLinkValue
		eventsLinkValue := api.Link(r, api.Path("patron_requests", request.ID, "events"), linkQuery)
		eventsLink = &eventsLinkValue
	}
	var illTransactionLink *string
	if request.RequesterReqID.Valid && request.RequesterReqID.String != "" {
		value := api.Link(r, api.Path("ill_transactions"), api.Query("requester_req_id", request.RequesterReqID.String))
		illTransactionLink = &value
	}

	pr := proapi.PatronRequest{
		Id:                   request.ID,
		CreatedAt:            request.CreatedAt.Time,
		State:                string(request.State),
		Side:                 string(request.Side),
		Patron:               toString(request.Patron),
		RequesterSymbol:      toString(request.RequesterSymbol),
		SupplierSymbol:       toString(request.SupplierSymbol),
		IllRequest:           request.IllRequest,
		RequesterRequestId:   toString(request.RequesterReqID),
		NeedsAttention:       request.NeedsAttention,
		HasCost:              request.HasCost,
		LastAction:           toString(request.LastAction),
		LastActionOutcome:    toString(request.LastActionOutcome),
		LastActionResult:     toString(request.LastActionResult),
		Items:                &items,
		NotificationsLink:    notificationsLink,
		ItemsLink:            itemsLink,
		AvailableActionsLink: availableActionsLink,
		IllTransactionLink:   illTransactionLink,
		EventsLink:           eventsLink,
		TerminalState:        request.TerminalState,
	}
	if request.UpdatedAt.Valid {
		pr.UpdatedAt = &request.UpdatedAt.Time
	}
	if request.IllResponse.StatusInfo.Status != "" { // If there is status that mean that message is not empty
		pr.IllResponse = &request.IllResponse
	}
	return pr
}

func toString(text pgtype.Text) *string {
	var value *string
	if text.Valid {
		value = &text.String
	}
	return value
}

func (a *PatronRequestApiHandler) parseAndValidateIllRequest(
	ctx common.ExtendedContext,
	request *proapi.CreatePatronRequest,
	creationTime time.Time,
) (iso18626.Request, string, error) {
	if request.RequesterSymbol == nil || *request.RequesterSymbol == "" {
		return iso18626.Request{}, "", fmt.Errorf("%w: requesterSymbol must be specified", errInvalidPatronRequest)
	}
	reqSymbolType, reqSymbolValue, err := parseAgencySymbol(*request.RequesterSymbol)
	if err != nil {
		return iso18626.Request{}, "", fmt.Errorf("%w: requesterSymbol: %w", errInvalidPatronRequest, err)
	}
	var requesterReqId string
	if request.Id != nil {
		requesterReqId = *request.Id
	} else {
		hrid, err := a.prRepo.GetNextHrid(ctx, reqSymbolValue)
		if err != nil {
			return iso18626.Request{}, "", err
		}
		requesterReqId = hrid
	}
	illRequest, err := prepareAndValidateIllRequest(
		request.IllRequest,
		reqSymbolType,
		reqSymbolValue,
		requesterReqId,
		creationTime,
	)
	if err != nil {
		return iso18626.Request{}, "", err
	}

	return illRequest, requesterReqId, nil
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

func parseAgencySymbol(symbol string) (string, string, error) {
	scheme, value, ok := strings.Cut(symbol, ":")
	if !ok || scheme == "" || value == "" {
		return "", "", fmt.Errorf("expected format SCHEME:VALUE, got %q", symbol)
	}
	return scheme, value, nil
}

func prepareAndValidateIllRequest(
	rawIllRequest iso18626.Request,
	reqSymbolType string,
	reqSymbolValue string,
	requesterReqId string,
	creationTime time.Time,
) (iso18626.Request, error) {
	if reflect.ValueOf(rawIllRequest).IsZero() {
		return iso18626.Request{}, fmt.Errorf("%w: illRequest must not be empty", errInvalidPatronRequest)
	}
	illRequest := rawIllRequest
	suppSymbolType, suppSymbolValue, err := parseAgencySymbol(brokerSymbol)
	if err != nil {
		return iso18626.Request{}, fmt.Errorf("invalid BROKER_SYMBOL %q: %w", brokerSymbol, err)
	}
	illRequest.Header.RequestingAgencyId = iso18626.TypeAgencyId{
		AgencyIdType:  iso18626.TypeSchemeValuePair{Text: reqSymbolType},
		AgencyIdValue: reqSymbolValue,
	}
	illRequest.Header.SupplyingAgencyId = iso18626.TypeAgencyId{
		AgencyIdType:  iso18626.TypeSchemeValuePair{Text: suppSymbolType},
		AgencyIdValue: suppSymbolValue,
	}
	illRequest.Header.Timestamp = utils.XSDDateTime{Time: creationTime}
	illRequest.Header.RequestingAgencyRequestId = requesterReqId
	if err = validateIllRequest(illRequest); err != nil {
		return iso18626.Request{}, fmt.Errorf("%w: invalid illRequest: %w", errInvalidPatronRequest, err)
	}
	return illRequest, nil
}

func buildDbPatronRequest(
	request *proapi.CreatePatronRequest,
	tenant *string,
	creationTime pgtype.Timestamp,
	requesterReqId string,
	illRequest iso18626.Request,
) pr_db.PatronRequest {
	return pr_db.PatronRequest{
		ID:              requesterReqId,
		CreatedAt:       creationTime,
		State:           prservice.BorrowerStateNew,
		Side:            prservice.SideBorrowing,
		Patron:          getDbText(request.Patron),
		RequesterSymbol: getDbText(request.RequesterSymbol),
		SupplierSymbol:  getDbText(nil),
		IllRequest:      illRequest,
		Tenant:          getDbText(tenant),
		RequesterReqID:  getDbText(&requesterReqId),
		Language:        pr_db.LANGUAGE,
		Items:           []pr_db.PrItem{},
		TerminalState:   false,
		// LastAction, LastActionOutcome and LastActionResult are not set on creation
		// they will be updated when the first action is executed.
	}
}

func validateIllRequest(request iso18626.Request) error {
	requestForValidation := request
	if requestForValidation.Header.MultipleItemRequestId == "" {
		//schema workaround
		requestForValidation.Header.MultipleItemRequestId = "#empty"
	}
	return illRequestValidator.Struct(requestForValidation)
}

func toApiItem(item pr_db.Item) proapi.PrItem {
	return proapi.PrItem{
		Id:         item.ID,
		Barcode:    item.Barcode,
		CallNumber: toString(item.CallNumber),
		ItemId:     toString(item.ItemID),
		Title:      toString(item.Title),
		CreatedAt:  item.CreatedAt.Time,
	}
}

func toApiPrItem(item pr_db.PrItem) proapi.PrItem {
	return proapi.PrItem{
		Id:         item.ID,
		Barcode:    item.Barcode,
		CallNumber: item.CallNumber,
		ItemId:     item.ItemID,
		Title:      item.Title,
		CreatedAt:  time.Time(item.CreatedAt),
	}
}

func toApiNotification(notification pr_db.Notification) (proapi.PrNotification, error) {
	var ackAt *time.Time
	if notification.AcknowledgedAt.Valid {
		t := notification.AcknowledgedAt.Time
		ackAt = &t
	}
	var cost *float64
	if notification.Cost.Valid {
		f, err := notification.Cost.Float64Value()
		if err != nil {
			return proapi.PrNotification{}, err
		}
		val := f.Float64
		cost = &val
	}
	var receipt *string
	if notification.Receipt != "" {
		r := string(notification.Receipt)
		receipt = &r
	}
	return proapi.PrNotification{
		Id:             notification.ID,
		FromSymbol:     notification.FromSymbol,
		ToSymbol:       notification.ToSymbol,
		Direction:      string(notification.Direction),
		Kind:           proapi.PrNotificationKind(notification.Kind),
		Note:           toString(notification.Note),
		Cost:           cost,
		Currency:       toString(notification.Currency),
		Condition:      toString(notification.Condition),
		Receipt:        receipt,
		CreatedAt:      notification.CreatedAt.Time,
		AcknowledgedAt: ackAt,
	}, nil
}

func toDbNotification(create proapi.CreatePrNotification, pr pr_db.PatronRequest) pr_db.Notification {
	fromSymbol := pr.RequesterSymbol.String
	toSymbol := pr.SupplierSymbol.String
	if pr.Side == prservice.SideLending {
		fromSymbol = pr.SupplierSymbol.String
		toSymbol = pr.RequesterSymbol.String
	}
	return pr_db.Notification{
		ID:         uuid.NewString(),
		PrID:       pr.ID,
		FromSymbol: fromSymbol,
		ToSymbol:   toSymbol,
		Direction:  pr_db.NotificationDirectionSent,
		Kind:       pr_db.NotificationKindNote,
		Note:       getDbText(&create.Note),
		CreatedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		AcknowledgedAt: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
	}
}
