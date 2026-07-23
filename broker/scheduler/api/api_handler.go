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
	eventRepo      events.EventRepo
	tenantResolver *tenant.TenantResolver
}

// NewSchedulerApiHandler creates a SchedulerApiHandler.
func NewSchedulerApiHandler(limitDefault int32, schedRepo sched_db.SchedRepo, eventRepo events.EventRepo, tenantResolver *tenant.TenantResolver) SchedulerApiHandler {
	return SchedulerApiHandler{
		limitDefault:   limitDefault,
		schedRepo:      schedRepo,
		eventRepo:      eventRepo,
		tenantResolver: tenantResolver,
	}
}

// GetBatchActions lists all batch actions for the resolved owner, with pagination.
func (h SchedulerApiHandler) GetBatchActions(w http.ResponseWriter, r *http.Request, params schedoapi.GetBatchActionsParams) {
	logParams := map[string]string{"method": "GetBatchActions"}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	owners, ok := h.resolveOwnerScope(ctx, w, r, params.Symbol)
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

	items, count, err := h.schedRepo.GetScheduledTasks(ctx, sched_db.GetScheduledTasksParams{
		Owners: owners,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}

	resp := schedoapi.BatchActions{
		About: schedoapi.About(brokerapi.CollectAboutData(count, offset, limit, r)),
		Items: toBatchActionList(r, items),
	}
	brokerapi.WriteJsonResponse(w, resp)
}

func (h SchedulerApiHandler) GetBatchActionsIdEvents(w http.ResponseWriter, r *http.Request, id string, params schedoapi.GetBatchActionsIdEventsParams) {
	_, ctx, done := h.getScheduledTask(w, r, "GetBatchActionsIdEvents", id, params.Symbol)
	if done {
		return
	}

	eventList, err := h.eventRepo.GetBatchActionEvents(ctx, id)
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}
	items := make([]schedoapi.Event, 0, len(eventList))
	for _, event := range eventList {
		var patronRequestID *string
		if event.PatronRequestID != "" && !events.IsSyntheticID(event.PatronRequestID) {
			patronRequestID = &event.PatronRequestID
		}
		items = append(items, schedoapi.Event(brokerapi.ToApiEvent(event, event.IllTransactionID, patronRequestID)))
	}
	brokerapi.WriteJsonResponse(w, schedoapi.Events{
		About: schedoapi.About{Count: int64(len(items))}, Items: items,
	})
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

	owner, ok := h.resolveConcreteOwner(ctx, w, r, params.Symbol)
	if !ok {
		return
	}

	next, err := sched_service.NextScheduleTime(create.Schedule)
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return
	}
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	taskId := uuid.New().String()
	paramsMap := make(map[string]any)
	if create.ActionParams != nil {
		paramsMap = *create.ActionParams
	}
	task, err := h.schedRepo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams{
		ID:        taskId,
		EventName: events.EventNameInvokeBatchAction,
		Schedule:  create.Schedule,
		Status:    sched_db.ScheduledTaskStatusPending,
		Owner:     owner,
		ActionData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{
					ActionName: string(create.ActionName),
					Selector:   create.BatchQuery,
					TaskId:     taskId,
					Owner:      owner,
				},
			},
			CustomData: paramsMap,
		},
		Title:     toPgText(create.Title),
		RunAt:     next,
		CreatedAt: now,
	})
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}

	w.Header().Set("Location", brokerapi.Link(r, brokerapi.Path("batch_actions", task.ID), nil))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toBatchAction(r, task))
}

// GetBatchActionsId returns a single batch action by ID.
func (h SchedulerApiHandler) GetBatchActionsId(w http.ResponseWriter, r *http.Request, id string, params schedoapi.GetBatchActionsIdParams) {
	task, _, done := h.getScheduledTask(w, r, "GetBatchActionsId", id, params.Symbol)
	if done {
		return
	}
	brokerapi.WriteJsonResponse(w, toBatchAction(r, task))
}

func (h SchedulerApiHandler) getScheduledTask(w http.ResponseWriter, r *http.Request, methodName string, id string, symbol *schedoapi.Symbol) (sched_db.ScheduledTask, common.ExtendedContext, bool) {
	logParams := map[string]string{"method": methodName, "id": id}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	owners, ok := h.resolveOwnerScope(ctx, w, r, symbol)
	if !ok {
		return sched_db.ScheduledTask{}, ctx, true
	}

	task, err := h.schedRepo.GetScheduledTaskById(ctx, id, owners)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			brokerapi.AddNotFoundError(w)
			return sched_db.ScheduledTask{}, ctx, true
		}
		brokerapi.AddInternalError(ctx, w, err)
		return sched_db.ScheduledTask{}, ctx, true
	}
	if task.ActionData.BatchActionData == nil {
		brokerapi.AddInternalError(ctx, w, errors.New("missing batchActionData"))
		return sched_db.ScheduledTask{}, ctx, true
	}
	return task, ctx, false
}

// DeleteBatchActionsId deletes a batch action by ID.
func (h SchedulerApiHandler) DeleteBatchActionsId(w http.ResponseWriter, r *http.Request, id string, params schedoapi.DeleteBatchActionsIdParams) {
	logParams := map[string]string{"method": "DeleteBatchActionsId", "id": id}
	ctx := common.CreateExtCtxWithArgs(r.Context(), &common.LoggerArgs{Other: logParams})

	owners, ok := h.resolveOwnerScope(ctx, w, r, params.Symbol)
	if !ok {
		return
	}

	err := h.schedRepo.WithTxFunc(ctx, func(schedRepo sched_db.SchedRepo) error {
		task, inErr := schedRepo.GetScheduledTaskById(ctx, id, owners)
		if inErr != nil {
			return inErr
		}
		return schedRepo.DeleteScheduledTask(ctx, task.ID, owners)
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

func (h SchedulerApiHandler) PutBatchActionsId(w http.ResponseWriter, r *http.Request, id string, params schedoapi.PutBatchActionsIdParams) {
	task, ctx, done := h.getScheduledTask(w, r, "PutBatchActionsId", id, params.Symbol)
	if done {
		return
	}
	if r.Body == nil || r.Body == http.NoBody {
		brokerapi.AddBadRequestError(ctx, w, errors.New("missing body"))
		return
	}
	var update schedoapi.UpdateBatchAction
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return
	}
	if update.Schedule == "" {
		brokerapi.AddBadRequestError(ctx, w, errors.New("schedule must not be empty"))
		return
	}
	if update.BatchQuery == "" {
		brokerapi.AddBadRequestError(ctx, w, errors.New("batchQuery must not be empty"))
		return
	}
	next, err := sched_service.NextScheduleTime(update.Schedule)
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return
	}
	task.Schedule = update.Schedule
	task.RunAt = next
	if task.ActionData.BatchActionData == nil {
		task.ActionData.BatchActionData = &events.BatchActionData{}
	}
	task.ActionData.BatchActionData.Selector = update.BatchQuery
	task.Title = toPgText(update.Title)

	if update.ActionParams != nil {
		task.ActionData.CustomData = *update.ActionParams
	}

	task, err = h.schedRepo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams(task))
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}

	brokerapi.WriteJsonResponse(w, toBatchAction(r, task))
}

func (h SchedulerApiHandler) PostBatchActionsIdDisable(w http.ResponseWriter, r *http.Request, id string, params schedoapi.PostBatchActionsIdDisableParams) {
	task, ctx, done := h.getScheduledTask(w, r, "PostBatchActionsIdDisable", id, params.Symbol)
	if done {
		return
	}
	task.Status = sched_db.ScheduledTaskStatusStopped
	_, err := h.schedRepo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams(task))
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h SchedulerApiHandler) PostBatchActionsIdEnable(w http.ResponseWriter, r *http.Request, id string, params schedoapi.PostBatchActionsIdEnableParams) {
	task, ctx, done := h.getScheduledTask(w, r, "PostBatchActionsIdEnable", id, params.Symbol)
	if done {
		return
	}
	task.Status = sched_db.ScheduledTaskStatusPending
	_, err := h.schedRepo.SaveScheduledTask(ctx, sched_db.SaveScheduledTaskParams(task))
	if err != nil {
		brokerapi.AddInternalError(ctx, w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveOwnerScope returns the owners the request may access. A nil scope
// means unrestricted master access.
func (h SchedulerApiHandler) resolveOwnerScope(ctx common.ExtendedContext, w http.ResponseWriter, r *http.Request, symbol *string) ([]string, bool) {
	t, err := h.tenantResolver.Resolve(ctx, r, symbol)
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return nil, false
	}
	owners, err := t.GetOwnedSymbols()
	if err != nil {
		brokerapi.AddBadRequestError(ctx, w, err)
		return nil, false
	}
	return owners, true
}

// resolveConcreteOwner resolves the single owner required when creating a task.
func (h SchedulerApiHandler) resolveConcreteOwner(ctx common.ExtendedContext, w http.ResponseWriter, r *http.Request, symbol *string) (string, bool) {
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
	if owner == "" {
		brokerapi.AddBadRequestError(ctx, w, errors.New("symbol must be specified when creating a batch action with master access"))
		return "", false
	}
	return owner, true
}

func toBatchAction(r *http.Request, task sched_db.ScheduledTask) schedoapi.BatchAction {
	actionData := task.ActionData.BatchActionData
	if actionData == nil { // Prevent panic for nil, should never happen
		actionData = &events.BatchActionData{}
	}
	active := task.Status != sched_db.ScheduledTaskStatusStopped
	resp := schedoapi.BatchAction{
		Id:         task.ID,
		Schedule:   task.Schedule,
		ActionName: schedoapi.BatchActionName(actionData.ActionName),
		CreatedAt:  task.CreatedAt.Time,
		BatchQuery: actionData.Selector,
		Active:     active,
		EventsLink: brokerapi.Link(r, brokerapi.Path("batch_actions", task.ID, "events"), nil),
	}
	if len(task.ActionData.CustomData) > 0 {
		resp.ActionParams = &task.ActionData.CustomData
	}
	if task.UpdatedAt.Valid {
		resp.UpdatedAt = &task.UpdatedAt.Time
	}
	if task.RunAt.Valid {
		resp.NextRun = &task.RunAt.Time
	}
	if task.Title.Valid {
		resp.Title = &task.Title.String
	}
	return resp
}

func toBatchActionList(r *http.Request, items []sched_db.ScheduledTask) []schedoapi.BatchAction {
	result := make([]schedoapi.BatchAction, 0, len(items))
	for _, task := range items {
		if task.ActionData.BatchActionData != nil {
			result = append(result, toBatchAction(r, task))
		}
	}
	return result
}

func toPgText(text *string) pgtype.Text {
	if text == nil {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: *text, Valid: true}
}
