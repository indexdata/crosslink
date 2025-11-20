package prapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	proapi "github.com/indexdata/crosslink/broker/patron_request/oapi"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"sync"
)

var waitingReqs = map[string]RequestWait{}

type PatronRequestApiHandler struct {
	prRepo   pr_db.PrRepo
	eventBus events.EventBus
}

func NewApiHandler(prRepo pr_db.PrRepo, eventBus events.EventBus) PatronRequestApiHandler {
	return PatronRequestApiHandler{
		prRepo:   prRepo,
		eventBus: eventBus,
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

func (a *PatronRequestApiHandler) PostPatronRequests(w http.ResponseWriter, r *http.Request) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "PostPatronRequests"},
	})
	var newPr proapi.CreatePatronRequest
	err := json.NewDecoder(r.Body).Decode(&newPr)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}

	pr, err := a.prRepo.SavePatronRequest(ctx, (pr_db.SavePatronRequestParams)(toDbPatronRequest(newPr)))
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}

	_, err = a.eventBus.CreateTask(pr.ID, events.EventNameInvokeAction, events.EventData{CommonEventData: events.CommonEventData{Action: &prservice.ActionValidate}}, events.EventDomainPatronRequest, nil)
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toApiPatronRequest(pr))
}

func (a *PatronRequestApiHandler) DeletePatronRequestsId(w http.ResponseWriter, r *http.Request, id string) {
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

func (a *PatronRequestApiHandler) GetPatronRequestsId(w http.ResponseWriter, r *http.Request, id string) {
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

func (a *PatronRequestApiHandler) PutPatronRequestsId(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPatronRequestsId", "id": id},
	})
	var updatePr proapi.UpdatePatronRequest
	err := json.NewDecoder(r.Body).Decode(&updatePr)
	if err != nil {
		addInternalError(ctx, w, err)
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
	if updatePr.Requester != nil {
		pr.Requester = getDbText(updatePr.Requester)
	}
	pr, err = a.prRepo.SavePatronRequest(ctx, (pr_db.SavePatronRequestParams)(pr))
	if err != nil {
		addInternalError(ctx, w, err)
		return
	}
	writeJsonResponse(w, toApiPatronRequest(pr))
}

func (a *PatronRequestApiHandler) GetPatronRequestsIdActions(w http.ResponseWriter, r *http.Request, id string) {
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
	writeJsonResponse(w, prservice.GetBorrowerActionsByState(pr.State))
}

func (a *PatronRequestApiHandler) PostPatronRequestsIdAction(w http.ResponseWriter, r *http.Request, id string) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), &common.LoggerArgs{
		Other: map[string]string{"method": "GetPatronRequestsIdActions", "id": id},
	})
	var action proapi.ExecuteAction
	err := json.NewDecoder(r.Body).Decode(&action)
	if err != nil {
		addInternalError(ctx, w, err)
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
	if !prservice.IsBorrowerActionAvailable(pr.State, action.Action) {
		addBadRequestError(ctx, w, errors.New("Action "+action.Action+" is not allowed for patron request "+id))
		return
	}

	data := events.EventData{CommonEventData: events.CommonEventData{Action: &action.Action}}
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
		State:           request.State,
		Side:            request.Side,
		Requester:       toString(request.Requester),
		BorrowingPeerId: toString(request.BorrowingPeerID),
		LendingPeerId:   toString(request.LendingPeerID),
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

func toDbPatronRequest(request proapi.CreatePatronRequest) pr_db.PatronRequest {
	var illRequest []byte
	if request.IllRequest != nil {
		illRequest = []byte(*request.IllRequest)
	}
	return pr_db.PatronRequest{
		ID:              getId(request.ID),
		Timestamp:       pgtype.Timestamp{Valid: true, Time: request.Timestamp},
		State:           prservice.BorrowerStateNew,
		Side:            prservice.SideBorrowing,
		Requester:       getDbText(request.Requester),
		BorrowingPeerID: getDbText(request.BorrowingPeerId),
		LendingPeerID:   getDbText(request.LendingPeerId),
		IllRequest:      illRequest,
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
