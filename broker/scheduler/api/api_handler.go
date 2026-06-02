package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	brokerapi "github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	sched_service "github.com/indexdata/crosslink/broker/scheduler/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// SchedulerApiHandler implements schedoapi.ServerInterface.
type SchedulerApiHandler struct {
	limitDefault   int32
	schedRepo      sched_db.SchedRepo
	tenantResolver *tenant.TenantResolver
}

// NewSchedulerApiHandler creates a SchedulerApiHandler.
func NewSchedulerApiHandler(limitDefault int32, schedRepo sched_db.SchedRepo, tenantResolver *tenant.TenantResolver) SchedulerApiHandler {
	return SchedulerApiHandler{
		limitDefault:   limitDefault,
		schedRepo:      schedRepo,
		tenantResolver: tenantResolver,
	}
}

// GetBatchActions lists all batch actions for the resolved owner, with pagination.
func (h SchedulerApiHandler) GetBatchActions(w http.ResponseWriter, r *http.Request, params schedoapi.GetBatchActionsParams) {
	logParams := map[string]string{"method": "GetBatchActions"}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	owner, ok := h.resolveOwner(ctx, w, r, params.Symbol)
	if !ok {
		return
	}

	limit := h.limitDefault
	if params.Limit != nil && *params.Limit > 0 {
		limit = *params.Limit
	}
	offset := int32(0)
	if params.Offset != nil && *params.Offset > 0 {
		offset = *params.Offset
	}

	items, count, err := h.schedRepo.GetBatchActions(ctx, sched_db.GetBatchActionsParams{
		Owner:  owner,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}

	resp := schedoapi.BatchActions{
		About: schedoapi.About(brokerapi.CollectAboutData(count, offset, limit, r)),
		Items: toBatchActionList(items),
	}
	brokerapi.WriteJsonResponse(w, resp)
}

// PostBatchActions creates a new batch action.
func (h SchedulerApiHandler) PostBatchActions(w http.ResponseWriter, r *http.Request, params schedoapi.PostBatchActionsParams) {
	logParams := map[string]string{"method": "PostBatchActions"}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	if r.Body == nil || r.Body == http.NoBody {
		brokerapi.AddBadRequestError(ctx, w, errors.New("missing body"))
		return
	}
	var create schedoapi.CreateBatchAction
	if err := json.NewDecoder(r.Body).Decode(&create); err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return
	}
	if !create.ActionName.Valid() {
		brokerapi.AddBadRequestError(ctx, w, errors.New("unknown actionName: "+string(create.ActionName)))
		return
	}
	if create.Schedule == "" {
		brokerapi.AddBadRequestError(ctx, w, errors.New("schedule must not be empty"))
		return
	}
	if create.BatchQuery == "" {
		brokerapi.AddBadRequestError(ctx, w, errors.New("batchQuery must not be empty"))
		return
	}

	owner, ok := h.resolveOwner(ctx, w, r, params.Symbol)
	if !ok {
		return
	}

	next, err := sched_service.NextCronTime(create.Schedule)
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return
	}
	var ba sched_db.BatchAction
	err = h.schedRepo.WithTxFunc(ctx, func(schedRepo sched_db.SchedRepo) error {
		id := uuid.NewString()
		now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
		taskId := uuid.New().String()
		paramsMap := make(map[string]any)
		if create.ActionParams != nil {
			paramsMap = *create.ActionParams
		}
		task, inErr := schedRepo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams{
			ID:        taskId,
			EventName: events.EventNameEmailPullslips,
			CronExpr:  create.Schedule,
			Status:    sched_db.ScheduledTaskStatusPending,
			Payload: events.EventData{
				CommonEventData: events.CommonEventData{
					BatchActionData: &events.BatchActionData{
						ActionName: string(create.ActionName),
						Selector:   create.BatchQuery,
						TaskId:     taskId,
					},
				},
				CustomData: paramsMap,
			},
			RunAt:     next,
			CreatedAt: now,
		})
		if inErr != nil {
			return inErr
		}
		ba, inErr = schedRepo.SaveBatchAction(ctx, sched_db.SaveBatchActionParams{
			ID:              id,
			Schedule:        create.Schedule,
			ActionName:      string(create.ActionName),
			BatchQuery:      create.BatchQuery,
			Owner:           owner,
			CreatedAt:       now,
			ScheduledTaskID: task.ID,
			ActionParams:    paramsMap,
		})
		return inErr
	})
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}

	w.Header().Set("Location", brokerapi.Link(r, brokerapi.Path("batch_actions", ba.ID), nil))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toBatchAction(ba))
}

// GetBatchActionsId returns a single batch action by ID.
func (h SchedulerApiHandler) GetBatchActionsId(w http.ResponseWriter, r *http.Request, id string, params schedoapi.GetBatchActionsIdParams) {
	logParams := map[string]string{"method": "GetBatchActionsId", "id": id}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	owner, ok := h.resolveOwner(ctx, w, r, params.Symbol)
	if !ok {
		return
	}

	ba, err := h.schedRepo.GetBatchActionById(ctx, id, owner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			brokerapi.AddNotFoundError(w)
			return
		}
		brokerapi.AddInternalError(ctx, w, err)
		return
	}
	brokerapi.WriteJsonResponse(w, toBatchAction(ba))
}

// DeleteBatchActionsId deletes a batch action by ID.
func (h SchedulerApiHandler) DeleteBatchActionsId(w http.ResponseWriter, r *http.Request, id string, params schedoapi.DeleteBatchActionsIdParams) {
	logParams := map[string]string{"method": "DeleteBatchActionsId", "id": id}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	owner, ok := h.resolveOwner(ctx, w, r, params.Symbol)
	if !ok {
		return
	}

	err := h.schedRepo.WithTxFunc(ctx, func(schedRepo sched_db.SchedRepo) error {
		ba, inErr := schedRepo.GetBatchActionById(ctx, id, owner)
		if inErr != nil {
			return inErr
		}
		task, inErr := schedRepo.GetScheduledTaskById(ctx, ba.ScheduledTaskID)
		if inErr != nil {
			return inErr
		}
		task.Status = sched_db.ScheduledTaskStatusStopped
		task, inErr = schedRepo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams(task))
		if inErr != nil {
			return inErr
		}
		return schedRepo.DeleteBatchAction(ctx, id, owner)
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			brokerapi.AddNotFoundError(w)
			return
		}
		brokerapi.AddInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveOwner resolves the tenant and returns the owner symbol.
// Writes an error response and returns false on failure.
func (h SchedulerApiHandler) resolveOwner(ctx common.ExtendedContext, w http.ResponseWriter, r *http.Request, symbol *string) (string, bool) {
	t, err := h.tenantResolver.Resolve(ctx, r, symbol)
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return "", false
	}
	owner, err := t.GetRequestSymbol()
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return "", false
	}
	return owner, true
}

func toBatchAction(ba sched_db.BatchAction) schedoapi.BatchAction {
	resp := schedoapi.BatchAction{
		Id:         ba.ID,
		Schedule:   ba.Schedule,
		ActionName: schedoapi.BatchActionActionName(ba.ActionName),
		CreatedAt:  ba.CreatedAt.Time,
	}
	if len(ba.ActionParams) > 0 {
		resp.ActionParams = &ba.ActionParams
	}
	if ba.UpdatedAt.Valid {
		resp.UpdatedAt = &ba.UpdatedAt.Time
	}
	return resp
}

func toBatchActionList(items []sched_db.BatchAction) []schedoapi.BatchAction {
	result := make([]schedoapi.BatchAction, 0, len(items))
	for _, ba := range items {
		result = append(result, toBatchAction(ba))
	}
	return result
}
