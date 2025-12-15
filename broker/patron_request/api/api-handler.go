package prapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	proapi "github.com/indexdata/crosslink/broker/patron_request/oapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var waitingReqs = map[string]RequestWait{}

type PatronRequestApiHandler struct {
	prRepo               pr_db.PrRepo
	eventBus             events.EventBus
	actionMappingService prservice.ActionMappingService
}

func NewApiHandler(prRepo pr_db.PrRepo, eventBus events.EventBus) PatronRequestApiHandler {
	return PatronRequestApiHandler{
		prRepo:               prRepo,
		eventBus:             eventBus,
		actionMappingService: prservice.ActionMappingService{},
	}
}

func (a *PatronRequestApiHandler) GetPatronRequests(w http.ResponseWriter, r *http.Request) {
	logParams := map[string]string{"method": "GetPatronRequests"}
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: logParams,
	})
	prs, err := a.prRepo.ListPatronRequests(ctx)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) { //DB error
		addInternalError(ctx, w, err)
		return
	}
	var responseItems []proapi.PatronRequest
	for _, pr := range prs {
		responseItems = append(responseItems, toApiPatronRequest(pr))
	}
	writeJsonResponse(w, responseItems)
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
	tenant := params.XOkapiTenant
	if tenant == nil {
		addBadRequestError(ctx, w, errors.New("X-Okapi-Tenant header is required"))
		return
	}
	pr, err := a.prRepo.SavePatronRequest(ctx, (pr_db.SavePatronRequestParams)(toDbPatronRequest(newPr, *tenant)))
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	action := prservice.ActionValidate
	_, err = a.eventBus.CreateTask(pr.ID, events.EventNameInvokeAction, events.EventData{CommonEventData: events.CommonEventData{Action: &action}}, events.EventDomainPatronRequest, nil)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiPatronRequest(pr))
}

func (a *PatronRequestApiHandler) DeletePatronRequestsId(w http.ResponseWriter, r *http.Request, id string, params proapi.DeletePatronRequestsIdParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "DeletePatronRequestsId", "id": id},
	})
	err := a.prRepo.WithTxFunc(ctx, func(repo pr_db.PrRepo) error {
		pr, inErr := repo.GetPatronRequestById(ctx, id)
		if inErr != nil {
			return inErr
		}
		return repo.DeletePatronRequest(ctx, pr.ID)
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

func (a *PatronRequestApiHandler) GetPatronRequestsId(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPatronRequestsId", "id": id},
	})
	pr, err := a.prRepo.GetPatronRequestById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	writeJsonResponse(w, toApiPatronRequest(pr))
}

func (a *PatronRequestApiHandler) GetPatronRequestsIdActions(w http.ResponseWriter, r *http.Request, id string, params proapi.GetPatronRequestsIdActionsParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPatronRequestsIdActions", "id": id},
	})
	pr, err := a.prRepo.GetPatronRequestById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}
	actions := a.actionMappingService.GetActionMapping(pr).GetActionsForPatronRequest(pr)
	writeJsonResponse(w, actions)
}

func (a *PatronRequestApiHandler) PostPatronRequestsIdAction(w http.ResponseWriter, r *http.Request, id string, params proapi.PostPatronRequestsIdActionParams) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPatronRequestsIdActions", "id": id},
	})
	var action proapi.ExecuteAction
	err := json.NewDecoder(r.Body).Decode(&action)
	if err != nil {
		addBadRequestError(ctx, w, err)
		return
	}
	pr, err := a.prRepo.GetPatronRequestById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			addNotFoundError(w)
			return
		} else {
			addInternalError(ctx, w, err)
			return
		}
	}

	if !a.actionMappingService.GetActionMapping(pr).IsActionAvailable(pr, pr_db.PatronRequestAction(action.Action)) {
		addBadRequestError(ctx, w, errors.New("Action "+action.Action+" is not allowed for patron request "+id))
		return
	}
	eventAction := pr_db.PatronRequestAction(action.Action)
	data := events.EventData{CommonEventData: events.CommonEventData{Action: &eventAction}}
	if action.ActionParams != nil {
		data.CustomData = *action.ActionParams
	}
	eventId, err := a.eventBus.CreateTask(pr.ID, events.EventNameInvokeAction, data, events.EventDomainPatronRequest, nil)
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
	resp := proapi.Error{
		Error: err.Error(),
	}
	ctx.Logger().Error("error serving api request", "error", err.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	_ = json.NewEncoder(w).Encode(resp)
}

func addBadRequestError(ctx common.ExtendedContext, w http.ResponseWriter, err error) {
	resp := proapi.Error{
		Error: err.Error(),
	}
	ctx.Logger().Error("error serving api request", "error", err.Error())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(resp)
}

func addNotFoundError(w http.ResponseWriter) {
	resp := proapi.Error{
		Error: "not found",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(resp)
}

func toApiPatronRequest(request pr_db.PatronRequest) proapi.PatronRequest {
	return proapi.PatronRequest{
		ID:              request.ID,
		Timestamp:       request.Timestamp.Time,
		State:           string(request.State),
		Side:            string(request.Side),
		Patron:          toString(request.Patron),
		RequesterSymbol: toString(request.RequesterSymbol),
		SupplierSymbol:  toString(request.SupplierSymbol),
		IllRequest:      toStringFromBytes(request.IllRequest),
	}
}

func toString(text pgtype.Text) *string {
	var value *string
	if text.Valid {
		value = &text.String
	}
	return value
}

func toStringFromBytes(bytes []byte) *string {
	var value *string
	if len(bytes) > 0 {
		v := string(bytes)
		value = &v
	}
	return value
}

func toDbPatronRequest(request proapi.CreatePatronRequest, tenant string) pr_db.PatronRequest {
	var illRequest []byte
	if request.IllRequest != nil {
		illRequest = []byte(*request.IllRequest)
	}
	return pr_db.PatronRequest{
		ID:              getId(request.ID),
		Timestamp:       pgtype.Timestamp{Valid: true, Time: request.Timestamp},
		State:           prservice.BorrowerStateNew,
		Side:            prservice.SideBorrowing,
		Patron:          getDbText(request.Patron),
		RequesterSymbol: getDbText(request.RequesterSymbol),
		SupplierSymbol:  getDbText(request.SupplierSymbol),
		IllRequest:      illRequest,
		Tenant:          getDbText(&tenant),
	}
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
