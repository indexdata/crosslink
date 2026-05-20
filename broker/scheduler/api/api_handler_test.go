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
func (m *MockSchedRepo) WithTxFunc(ctx common.ExtendedContext, fn func(sched_db.SchedRepo) error) error {
	return fn(m)
}

func (m *MockSchedRepo) SaveScheduledTask(ctx common.ExtendedContext, params sched_db.SaveScheduledTaskParams) (sched_db.ScheduledTask, error) {
	args := m.Called(params)
	return args.Get(0).(sched_db.ScheduledTask), args.Error(1)
}

func (m *MockSchedRepo) GetScheduledTaskById(ctx common.ExtendedContext, id string) (sched_db.ScheduledTask, error) {
	args := m.Called(id)
	return args.Get(0).(sched_db.ScheduledTask), args.Error(1)
}

func (m *MockSchedRepo) SaveBatchAction(ctx common.ExtendedContext, params sched_db.SaveBatchActionParams) (sched_db.BatchAction, error) {
	args := m.Called(params)
	return args.Get(0).(sched_db.BatchAction), args.Error(1)
}

func (m *MockSchedRepo) GetBatchActionById(ctx common.ExtendedContext, id, owner string) (sched_db.BatchAction, error) {
	args := m.Called(id, owner)
	return args.Get(0).(sched_db.BatchAction), args.Error(1)
}

func (m *MockSchedRepo) DeleteBatchAction(ctx common.ExtendedContext, id, owner string) error {
	args := m.Called(id, owner)
	return args.Error(0)
}

func (m *MockSchedRepo) GetBatchActions(ctx common.ExtendedContext, params sched_db.GetBatchActionsParams) ([]sched_db.BatchAction, int64, error) {
	args := m.Called(params)
	return args.Get(0).([]sched_db.BatchAction), args.Get(1).(int64), args.Error(2)
}

// ── helpers ───────────────────────────────────────────────────────────────────

const testSymbol = "ISIL:TEST"
const validCron = "0 6 * * 1"

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

func batchActionFixture(id string) sched_db.BatchAction {
	return sched_db.BatchAction{
		ID:              id,
		ActionName:      "email",
		Schedule:        validCron,
		BatchQuery:      "",
		Owner:           testSymbol,
		ScheduledTaskID: "task-" + id,
		CreatedAt:       pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func scheduledTaskFixture(id string) sched_db.ScheduledTask {
	return sched_db.ScheduledTask{
		ID:     id,
		Status: sched_db.ScheduledTaskStatusPending,
	}
}

// ── GetBatchActions ───────────────────────────────────────────────────────────

func TestGetBatchActions_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	items := []sched_db.BatchAction{batchActionFixture("ba-1"), batchActionFixture("ba-2")}
	repo.On("GetBatchActions", mock.MatchedBy(func(p sched_db.GetBatchActionsParams) bool {
		return p.Owner == testSymbol
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
	assert.Equal(t, "ba-1", resp.Items[0].Id)
	repo.AssertExpectations(t)
}

func TestGetBatchActions_EmptyList(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetBatchActions", mock.Anything).Return([]sched_db.BatchAction{}, int64(0), nil)

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
	repo.On("GetBatchActions", sched_db.GetBatchActionsParams{
		Owner:  testSymbol,
		Limit:  limit,
		Offset: offset,
	}).Return([]sched_db.BatchAction{}, int64(20), nil)

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
	repo.On("GetBatchActions", mock.Anything).Return([]sched_db.BatchAction{}, int64(0), errors.New("db error"))

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
	ba := batchActionFixture("ba-new")
	task := scheduledTaskFixture("task-new")

	repo.On("SaveScheduledTask", mock.Anything).Return(task, nil)
	repo.On("SaveBatchAction", mock.Anything).Return(ba, nil)

	h := newHandler(repo)
	body := `{"actionName":"email","batchQuery":"title=test","schedule":"` + validCron + `"}`
	req := newReq(http.MethodPost, body)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusCreated, rr.Code)
	assert.NotEmpty(t, rr.Header().Get("Location"))
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	var resp schedoapi.BatchAction
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ba-new", resp.Id)
	repo.AssertExpectations(t)
}

func TestPostBatchActions_MissingBody(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, "")
	req.Body = nil
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostBatchActions_InvalidJSON(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, `{not-json}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostBatchActions_InvalidActionName(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, `{"actionName":"unknown","schedule":"`+validCron+`"}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostBatchActions_EmptySchedule(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, `{"actionName":"email","schedule":""}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostBatchActions_InvalidCronExpression(t *testing.T) {
	h := newHandler(new(MockSchedRepo))
	req := newReq(http.MethodPost, `{"actionName":"email","schedule":"not-a-cron"}`)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestPostBatchActions_SaveScheduledTaskError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("SaveScheduledTask", mock.Anything).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	body := `{"actionName":"email","batchQuery":"title=test","schedule":"` + validCron + `"}`
	req := newReq(http.MethodPost, body)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

func TestPostBatchActions_SaveBatchActionError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("SaveScheduledTask", mock.Anything).Return(scheduledTaskFixture("task-1"), nil)
	repo.On("SaveBatchAction", mock.Anything).Return(sched_db.BatchAction{}, errors.New("db error"))

	h := newHandler(repo)
	body := `{"actionName":"email","batchQuery":"title=test","schedule":"` + validCron + `"}`
	req := newReq(http.MethodPost, body)
	rr := httptest.NewRecorder()
	h.PostBatchActions(rr, req, schedoapi.PostBatchActionsParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

// ── GetBatchActionsId ─────────────────────────────────────────────────────────

func TestGetBatchActionsId_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	ba := batchActionFixture("ba-1")
	ba.UpdatedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	repo.On("GetBatchActionById", "ba-1", testSymbol).Return(ba, nil)

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActionsId(rr, req, "ba-1", schedoapi.GetBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "application/json")
	var resp schedoapi.BatchAction
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Equal(t, "ba-1", resp.Id)
	assert.NotNil(t, resp.UpdatedAt)
	repo.AssertExpectations(t)
}

func TestGetBatchActionsId_NotFound(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetBatchActionById", "missing", testSymbol).Return(sched_db.BatchAction{}, pgx.ErrNoRows)

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActionsId(rr, req, "missing", schedoapi.GetBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	repo.AssertExpectations(t)
}

func TestGetBatchActionsId_DBError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetBatchActionById", "ba-err", testSymbol).Return(sched_db.BatchAction{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodGet, "")
	rr := httptest.NewRecorder()
	h.GetBatchActionsId(rr, req, "ba-err", schedoapi.GetBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

// ── DeleteBatchActionsId ─────────────────────────────────────────────────────────────

func TestDeleteBatchActionsId_OK(t *testing.T) {
	repo := new(MockSchedRepo)
	ba := batchActionFixture("ba-1")
	task := scheduledTaskFixture("task-ba-1")
	repo.On("GetBatchActionById", "ba-1", testSymbol).Return(ba, nil)
	repo.On("GetScheduledTaskById", ba.ScheduledTaskID).Return(task, nil)
	repo.On("SaveScheduledTask", mock.Anything).Return(task, nil)
	repo.On("DeleteBatchAction", "ba-1", testSymbol).Return(nil)

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "ba-1", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNoContent, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_NotFound(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetBatchActionById", "missing", testSymbol).Return(sched_db.BatchAction{}, pgx.ErrNoRows)

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "missing", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusNotFound, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_GetBatchActionError(t *testing.T) {
	repo := new(MockSchedRepo)
	repo.On("GetBatchActionById", "ba-err", testSymbol).Return(sched_db.BatchAction{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "ba-err", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_GetScheduledTaskError(t *testing.T) {
	repo := new(MockSchedRepo)
	ba := batchActionFixture("ba-1")
	repo.On("GetBatchActionById", "ba-1", testSymbol).Return(ba, nil)
	repo.On("GetScheduledTaskById", ba.ScheduledTaskID).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "ba-1", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_SaveTaskError(t *testing.T) {
	repo := new(MockSchedRepo)
	ba := batchActionFixture("ba-1")
	task := scheduledTaskFixture("task-ba-1")
	repo.On("GetBatchActionById", "ba-1", testSymbol).Return(ba, nil)
	repo.On("GetScheduledTaskById", ba.ScheduledTaskID).Return(task, nil)
	repo.On("SaveScheduledTask", mock.Anything).Return(sched_db.ScheduledTask{}, errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "ba-1", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}

func TestDeleteBatchActionsId_DeleteError(t *testing.T) {
	repo := new(MockSchedRepo)
	ba := batchActionFixture("ba-1")
	task := scheduledTaskFixture("task-ba-1")
	repo.On("GetBatchActionById", "ba-1", testSymbol).Return(ba, nil)
	repo.On("GetScheduledTaskById", ba.ScheduledTaskID).Return(task, nil)
	repo.On("SaveScheduledTask", mock.Anything).Return(task, nil)
	repo.On("DeleteBatchAction", "ba-1", testSymbol).Return(errors.New("db error"))

	h := newHandler(repo)
	req := newReq(http.MethodDelete, "")
	rr := httptest.NewRecorder()
	h.DeleteBatchActionsId(rr, req, "ba-1", schedoapi.DeleteBatchActionsIdParams{Symbol: symPtr(testSymbol)})

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	repo.AssertExpectations(t)
}
