package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// ── mock ──────────────────────────────────────────────────────────────────────

type MockSchedRepo struct {
	mock.Mock
	sched_db.SchedRepo // satisfies unimplemented interface methods
}

// WithTxFunc calls fn(mock) directly, simulating a pass-through transaction.
func (m *MockSchedRepo) WithTxFunc(_ common.ExtendedContext, fn func(sched_db.SchedRepo) error) error {
	return fn(m)
}

func (m *MockSchedRepo) SaveScheduledTask(_ common.ExtendedContext, params sched_db.SaveScheduledTaskParams) (sched_db.ScheduledTask, error) {
	args := m.Called(params)
	ret := args.Get(0)
	if fn, ok := ret.(func(sched_db.SaveScheduledTaskParams) sched_db.ScheduledTask); ok {
		return fn(params), args.Error(1)
	}
	return ret.(sched_db.ScheduledTask), args.Error(1)
}

func (m *MockSchedRepo) GetScheduledTaskByIdAndOwner(_ common.ExtendedContext, id, owner string) (sched_db.ScheduledTask, error) {
	args := m.Called(id, owner)
	return args.Get(0).(sched_db.ScheduledTask), args.Error(1)
}

func (m *MockSchedRepo) DeleteScheduledTask(_ common.ExtendedContext, id, owner string) error {
	args := m.Called(id, owner)
	return args.Error(0)
}

func (m *MockSchedRepo) GetScheduledTasks(_ common.ExtendedContext, params sched_db.GetScheduledTasksParams) ([]sched_db.ScheduledTask, int64, error) {
	args := m.Called(params)
	return args.Get(0).([]sched_db.ScheduledTask), args.Get(1).(int64), args.Error(2)
}

// ── helpers ───────────────────────────────────────────────────────────────────

const testSymbol = "ISIL:TEST"
const validRrule = "FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0;BYSECOND=0"

func newHandler(repo sched_db.SchedRepo) SchedulerApiHandler {
	return NewSchedulerApiHandler(10, repo, tenant.NewResolver())
}

func newReq(method, body string) *http.Request {
	if body != "" {
		return httptest.NewRequest(method, "/batch_actions", strings.NewReader(body))
	}
	return httptest.NewRequest(method, "/batch_actions", nil)
}

func symPtr(s string) *string { return &s }

func tstz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func scheduledTaskFixture(id string) sched_db.ScheduledTask {
	now := time.Now().UTC()
	return sched_db.ScheduledTask{
		ID:        id,
		EventName: events.EventNameInvokeBatchAction,
		Schedule:  validRrule,
		ActionData: events.EventData{
			CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{
					ActionName: string(schedoapi.BatchActionActionNameEmailPullslips),
					Selector:   "title=test",
					TaskId:     id,
					Owner:      testSymbol,
				},
			},
		},
		RunAt:     tstz(now.Add(time.Hour)),
		Status:    sched_db.ScheduledTaskStatusPending,
		Owner:     testSymbol,
		CreatedAt: tstz(now.Add(-time.Hour)),
		UpdatedAt: tstz(now),
	}
}

func saveScheduledTaskReturn(params sched_db.SaveScheduledTaskParams) sched_db.ScheduledTask {
	return sched_db.ScheduledTask(params)
}

func assertErrorStatus(t *testing.T, rr *httptest.ResponseRecorder, status int) {
	t.Helper()
	assert.Equal(t, status, rr.Code)
	assert.NotEmpty(t, rr.Body.String())
}

// ── GetBatchActions ───────────────────────────────────────────────────────────

func TestGetBatchActions_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	items := []sched_db.ScheduledTask{scheduledTaskFixture("task-1"), scheduledTaskFixture("task-2")}
	repo.On("GetScheduledTasks", mock.MatchedBy(func(p sched_db.GetScheduledTasksParams) bool {
		return p.Owner == testSymbol && p.Limit == 10 && p.Offset == 0
	})).Return(items, int64(2), nil)

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActions(rr, req, schedoapi.GetBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	var resp schedoapi.BatchActions
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.About.Count)
	assert.Len(t, resp.Items, 2)
	assert.Equal(t, "task-1", resp.Items[0].Id)
	assert.Equal(t, schedoapi.BatchActionActionNameEmailPullslips, resp.Items[0].ActionName)
	assert.Equal(t, "title=test", resp.Items[0].BatchQuery)
	assert.True(t, resp.Items[0].Active)
	assert.NotNil(t, resp.Items[0].NextRun)
	repo.AssertExpectations(t)
}

func TestGetBatchActions_EmptyList(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTasks", mock.Anything).Return([]sched_db.ScheduledTask{}, int64(0), nil)

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActions(rr, req, schedoapi.GetBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp schedoapi.BatchActions
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Items)
	repo.AssertExpectations(t)
}

func TestGetBatchActions_WithPagination(t *testing.T) {
	repo := new(MockSchedRepo)
	limit := int32(5)
	offset := int32(10)
	repo.On("GetScheduledTasks", sched_db.GetScheduledTasksParams{
		Owner:  testSymbol,
		Limit:  limit,
		Offset: offset,
	}).Return([]sched_db.ScheduledTask{}, int64(20), nil)

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActions(rr, req, schedoapi.GetBatchActionsParams{
		Symbol: symPtr(testSymbol),
		Limit:  &limit,
		Offset: &offset,
	})

	assert.Equal(t, http.StatusOK, rr.Code)
	repo.AssertExpectations(t)
}

func TestGetBatchActions_DBError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTasks", mock.Anything).Return([]sched_db.ScheduledTask{}, int64(0), errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActions(rr, req, schedoapi.GetBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

// ── PostBatchActions ──────────────────────────────────────────────────────────

func TestPostBatchActions_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	before := time.Now().UTC()

	repo.On("SaveScheduledTask", mock.MatchedBy(func(p sched_db.SaveScheduledTaskParams) bool {
		return p.ID != "" &&
			p.EventName == events.EventNameInvokeBatchAction &&
			p.Schedule == validRrule &&
			p.Status == sched_db.ScheduledTaskStatusPending &&
			p.Owner == testSymbol &&
			p.RunAt.Valid && p.RunAt.Time.After(before) &&
			p.CreatedAt.Valid &&
			p.ActionData.BatchActionData != nil &&
			p.ActionData.BatchActionData.ActionName == string(schedoapi.BatchActionActionNameEmailPullslips) &&
			p.ActionData.BatchActionData.Selector == "title=test" &&
			p.ActionData.BatchActionData.TaskId == p.ID &&
			p.ActionData.BatchActionData.Owner == testSymbol &&
			len(p.ActionData.CustomData) == 0
	})).Return(saveScheduledTaskReturn, nil)

	h := newHandler(repo)
	body := `{"actionName":"email-pullslips","batchQuery":"title=test","schedule":"` + validRrule + `"}`
	req := newReq(http.MethodPost, body)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("Location"))
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	var resp schedoapi.BatchAction
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Id)
	assert.Equal(t, validRrule, resp.Schedule)
	assert.Equal(t, "title=test", resp.BatchQuery)
	assert.True(t, resp.Active)
	assert.NotNil(t, resp.NextRun)
	repo.AssertExpectations(t)
}

func TestPostBatchActions_ValidDailySchedule_ComputesDynamicRunAt(t *testing.T) {
	repo := new(MockSchedRepo)
	before := time.Now().UTC()

	repo.On("SaveScheduledTask", mock.MatchedBy(func(p sched_db.SaveScheduledTaskParams) bool {
		return p.Schedule == "FREQ=DAILY" &&
			p.RunAt.Valid &&
			p.RunAt.Time.After(before.Add(23*time.Hour)) &&
			p.RunAt.Time.Before(before.Add(25*time.Hour))
	})).Return(saveScheduledTaskReturn, nil)

	h := newHandler(repo)
	req := newReq(http.MethodPost, `{"actionName":"email-pullslips","batchQuery":"title=test","schedule":"FREQ=DAILY"}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusCreated, rr.Code)
	repo.AssertExpectations(t)
}

func TestPostBatchActions_ActionParamsPersisted(t *testing.T) {
	repo := new(MockSchedRepo)

	repo.On("SaveScheduledTask", mock.MatchedBy(func(p sched_db.SaveScheduledTaskParams) bool {
		return p.ActionData.CustomData["delivery"] == "email" && p.ActionData.CustomData["max"] == float64(3)
	})).Return(saveScheduledTaskReturn, nil)

	h := newHandler(repo)
	req := newReq(http.MethodPost, `{"actionName":"email-pullslips","batchQuery":"title=test","schedule":"`+validRrule+`","actionParams":{"delivery":"email","max":3}}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusCreated, rr.Code)
	var resp schedoapi.BatchAction
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.NotNil(t, resp.ActionParams)
	assert.Equal(t, "email", (*resp.ActionParams)["delivery"])
	repo.AssertExpectations(t)
}

func TestPostBatchActions_MissingBody(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, "")
	req.Body = nil
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
}

func TestPostBatchActions_InvalidJSON(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, `{not-json}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
}

func TestPostBatchActions_InvalidActionName(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, `{"actionName":"unknown","batchQuery":"title=test","schedule":"`+validRrule+`"}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
}

func TestPostBatchActions_EmptySchedule(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, `{"actionName":"email-pullslips","batchQuery":"title=test","schedule":""}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
}

func TestPostBatchActions_MissingBatchQuery(t *testing.T) {
	repo := new(MockSchedRepo)
	h := newHandler(repo)
	req := newReq(http.MethodPost, `{"actionName":"email-pullslips","schedule":"`+validRrule+`"}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
	repo.AssertNotCalled(t, "SaveScheduledTask", mock.Anything)
}

func TestPostBatchActions_InvalidSchedule(t *testing.T) {
	repo := new(MockSchedRepo)
	h := newHandler(repo)
	req := newReq(http.MethodPost, `{"actionName":"email-pullslips","batchQuery":"title=test","schedule":"not-a-rrule"}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
	repo.AssertNotCalled(t, "SaveScheduledTask", mock.Anything)
}

func TestPostBatchActions_CountOneScheduleReturnsBadRequest(t *testing.T) {
	repo := new(MockSchedRepo)
	h := newHandler(repo)
	req := newReq(http.MethodPost, `{"actionName":"email-pullslips","batchQuery":"title=test","schedule":"FREQ=DAILY;COUNT=1"}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
	repo.AssertNotCalled(t, "SaveScheduledTask", mock.Anything)
}

func TestPostBatchActions_SaveScheduledTaskError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("SaveScheduledTask", mock.Anything).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	body := `{"actionName":"email-pullslips","batchQuery":"title=test","schedule":"` + validRrule + `"}`
	req := newReq(http.MethodPost, body)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

// ── GetBatchActionsId ─────────────────────────────────────────────────────────

func TestGetBatchActionsId_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	task := scheduledTaskFixture("task-1")
	task.ActionData.CustomData = map[string]any{"delivery": "email"}
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(task, nil)

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActionsId(rr, req, "task-1", schedoapi.GetBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	var resp schedoapi.BatchAction
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "task-1", resp.Id)
	assert.Equal(t, "title=test", resp.BatchQuery)
	assert.True(t, resp.Active)
	assert.NotNil(t, resp.UpdatedAt)
	assert.NotNil(t, resp.NextRun)
	assert.NotNil(t, resp.ActionParams)
	repo.AssertExpectations(t)
}

func TestGetBatchActionsId_NotFound(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "missing", testSymbol).Return(sched_db.ScheduledTask{}, pgx.ErrNoRows)

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActionsId(rr, req, "missing", schedoapi.GetBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	repo.AssertExpectations(t)
}

func TestGetBatchActionsId_DBError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "task-err", testSymbol).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActionsId(rr, req, "task-err", schedoapi.GetBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

// ── PutBatchActionsId ─────────────────────────────────────────────────────────

func TestPutBatchActionsId_OK_RecomputesRunAtAndPersistsActionData(t *testing.T) {
	repo := new(MockSchedRepo)
	task := scheduledTaskFixture("task-1")
	oldRunAt := task.RunAt.Time
	newSchedule := "FREQ=DAILY"
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(task, nil)
	repo.On("SaveScheduledTask", mock.MatchedBy(func(p sched_db.SaveScheduledTaskParams) bool {
		return p.ID == "task-1" &&
			p.Schedule == newSchedule &&
			p.RunAt.Valid && p.RunAt.Time.After(oldRunAt) &&
			p.ActionData.BatchActionData != nil &&
			p.ActionData.BatchActionData.Selector == "author=doe" &&
			p.ActionData.CustomData["delivery"] == "email"
	})).Return(saveScheduledTaskReturn, nil)

	h := newHandler(repo)
	req := newReq(http.MethodPut, `{"batchQuery":"author=doe","schedule":"`+newSchedule+`","actionParams":{"delivery":"email"}}`)
	rr := httptest.NewRecorder()
	h.PutBatchActionsId(rr, req, "task-1", schedoapi.PutBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp schedoapi.BatchAction
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, newSchedule, resp.Schedule)
	assert.Equal(t, "author=doe", resp.BatchQuery)
	assert.NotNil(t, resp.NextRun)
	repo.AssertExpectations(t)
}

func TestPutBatchActionsId_InvalidSchedule(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(scheduledTaskFixture("task-1"), nil)

	h := newHandler(repo)
	req := newReq(http.MethodPut, `{"batchQuery":"title=test","schedule":"not-a-rrule"}`)
	rr := httptest.NewRecorder()
	h.PutBatchActionsId(rr, req, "task-1", schedoapi.PutBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
	repo.AssertNotCalled(t, "SaveScheduledTask", mock.Anything)
	repo.AssertExpectations(t)
}

func TestPutBatchActionsId_MissingBody(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(scheduledTaskFixture("task-1"), nil)

	h := newHandler(repo)
	req := newReq(http.MethodPut, "")
	req.Body = nil
	rr := httptest.NewRecorder()
	h.PutBatchActionsId(rr, req, "task-1", schedoapi.PutBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assertErrorStatus(t, rr, http.StatusBadRequest)
	repo.AssertNotCalled(t, "SaveScheduledTask", mock.Anything)
	repo.AssertExpectations(t)
}

func TestPutBatchActionsId_SaveError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(scheduledTaskFixture("task-1"), nil)
	repo.On("SaveScheduledTask", mock.Anything).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodPut, `{"batchQuery":"title=test","schedule":"FREQ=DAILY"}`)
	rr := httptest.NewRecorder()
	h.PutBatchActionsId(rr, req, "task-1", schedoapi.PutBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

// ── DeleteBatchActionsId ──────────────────────────────────────────────────────

func TestDeleteBatchActionsId_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	task := scheduledTaskFixture("task-1")
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(task, nil)
	repo.On("DeleteScheduledTask", "task-1", testSymbol).Return(nil)

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "task-1", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNoContent, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_NotFound(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "missing", testSymbol).Return(sched_db.ScheduledTask{}, pgx.ErrNoRows)

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "missing", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_GetScheduledTaskError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "task-err", testSymbol).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "task-err", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_DeleteError(t *testing.T) {
	repo := new(MockSchedRepo)
	task := scheduledTaskFixture("task-1")
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(task, nil)
	repo.On("DeleteScheduledTask", "task-1", testSymbol).Return(errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "task-1", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

// ── Enable / Disable ─────────────────────────────────────────────────────────

func TestPostBatchActionsIdDisable_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	task := scheduledTaskFixture("task-1")
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(task, nil)
	repo.On("SaveScheduledTask", mock.MatchedBy(func(p sched_db.SaveScheduledTaskParams) bool {
		return p.ID == "task-1" && p.Status == sched_db.ScheduledTaskStatusStopped
	})).Return(saveScheduledTaskReturn, nil)

	h := newHandler(repo)
	req := newReq(http.MethodPost, "")
	rr := httptest.NewRecorder()
	h.PostBatchActionsIdDisable(rr, req, "task-1", schedoapi.PostBatchActionsIdDisableParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNoContent, rr.Code)
	repo.AssertExpectations(t)
}

func TestPostBatchActionsIdEnable_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	task := scheduledTaskFixture("task-1")
	task.Status = sched_db.ScheduledTaskStatusStopped
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(task, nil)
	repo.On("SaveScheduledTask", mock.MatchedBy(func(p sched_db.SaveScheduledTaskParams) bool {
		return p.ID == "task-1" && p.Status == sched_db.ScheduledTaskStatusPending
	})).Return(saveScheduledTaskReturn, nil)

	h := newHandler(repo)
	req := newReq(http.MethodPost, "")
	rr := httptest.NewRecorder()
	h.PostBatchActionsIdEnable(rr, req, "task-1", schedoapi.PostBatchActionsIdEnableParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNoContent, rr.Code)
	repo.AssertExpectations(t)
}

func TestPostBatchActionsIdDisable_SaveError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetScheduledTaskByIdAndOwner", "task-1", testSymbol).Return(scheduledTaskFixture("task-1"), nil)
	repo.On("SaveScheduledTask", mock.Anything).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodPost, "")
	rr := httptest.NewRecorder()
	h.PostBatchActionsIdDisable(rr, req, "task-1", schedoapi.PostBatchActionsIdDisableParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}
