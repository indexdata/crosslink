package skd_service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	skd_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

var testCtx = common.CreateExtCtxWithArgs(context.Background(), nil)

func tstz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func invalidTstz() pgtype.Timestamptz {
	return pgtype.Timestamptz{Valid: false}
}

// ---------------------------------------------------------------------------
// Mock SkdRepo
// ---------------------------------------------------------------------------

type mockSkdRepo struct {
	claimResults []skd_db.ScheduledTask
	claimErrors  []error
	claimIndex   int
	savedTasks   []skd_db.SaveScheduledTaskParams
	saveError    error
	nextRunAt    pgtype.Timestamptz
	nextRunAtErr error
}

func (m *mockSkdRepo) WithTxFunc(ctx common.ExtendedContext, fn func(skd_db.SkdRepo) error) error {
	return fn(m)
}

func (m *mockSkdRepo) ClaimNextScheduledTask(_ common.ExtendedContext) (skd_db.ScheduledTask, error) {
	if m.claimIndex >= len(m.claimResults) {
		return skd_db.ScheduledTask{}, pgx.ErrNoRows
	}
	task := m.claimResults[m.claimIndex]
	var err error
	if m.claimIndex < len(m.claimErrors) {
		err = m.claimErrors[m.claimIndex]
	}
	m.claimIndex++
	return task, err
}

func (m *mockSkdRepo) SaveScheduledTask(_ common.ExtendedContext, p skd_db.SaveScheduledTaskParams) (skd_db.ScheduledTask, error) {
	m.savedTasks = append(m.savedTasks, p)
	return skd_db.ScheduledTask(p), m.saveError
}

func (m *mockSkdRepo) GetNextRunAt(_ common.ExtendedContext) (pgtype.Timestamptz, error) {
	return m.nextRunAt, m.nextRunAtErr
}

// ---------------------------------------------------------------------------
// Mock EventBus — only CreateTask is exercised by the scheduler
// ---------------------------------------------------------------------------

type mockEventBus struct {
	events.EventBus
	createTaskErr    error
	createdTaskNames []events.EventName
}

func (m *mockEventBus) CreateTask(_ string, name events.EventName, _ events.EventData, _ events.EventDomain, _ *string, _ events.SignalTarget) (string, error) {
	m.createdTaskNames = append(m.createdTaskNames, name)
	return "task-id", m.createTaskErr
}

// ---------------------------------------------------------------------------
// nextCronTime
// ---------------------------------------------------------------------------

func TestNextCronTime_ValidExpr(t *testing.T) {
	ts, err := nextCronTime("* * * * *") // every minute
	assert.NoError(t, err)
	assert.True(t, ts.Valid)
	assert.True(t, ts.Time.After(time.Now()))
}

func TestNextCronTime_InvalidExpr(t *testing.T) {
	_, err := nextCronTime("not-a-cron")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid cron expression")
}

func TestNextCronTime_SpecificSchedule(t *testing.T) {
	// "0 9 * * 1" = every Monday at 09:00 — just verify it's in the future
	ts, err := nextCronTime("0 9 * * 1")
	assert.NoError(t, err)
	assert.True(t, ts.Valid)
	assert.True(t, ts.Time.After(time.Now()))
}

// ---------------------------------------------------------------------------
// waitUntil
// ---------------------------------------------------------------------------

func TestWaitUntil_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	extCtx := common.CreateExtCtxWithArgs(ctx, nil)
	cancel()

	result := waitUntil(extCtx, invalidTstz(), make(chan struct{}), 10*time.Second)
	assert.False(t, result, "should return false when context is cancelled")
}

func TestWaitUntil_NotifyWakes(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	ch := make(chan struct{}, 1)
	ch <- struct{}{} // pre-signal

	result := waitUntil(extCtx, invalidTstz(), ch, 10*time.Second)
	assert.True(t, result)
}

func TestWaitUntil_FallbackElapsed(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	start := time.Now()
	result := waitUntil(extCtx, invalidTstz(), make(chan struct{}), 20*time.Millisecond)
	assert.True(t, result)
	assert.WithinDuration(t, start.Add(20*time.Millisecond), time.Now(), 50*time.Millisecond)
}

func TestWaitUntil_NextRunAtSooner(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	nextRunAt := tstz(time.Now().Add(20 * time.Millisecond))
	start := time.Now()
	result := waitUntil(extCtx, nextRunAt, make(chan struct{}), 10*time.Second)
	assert.True(t, result)
	assert.WithinDuration(t, start.Add(20*time.Millisecond), time.Now(), 50*time.Millisecond)
}

func TestWaitUntil_NextRunAtAlreadyOverdue(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	nextRunAt := tstz(time.Now().Add(-1 * time.Second))
	result := waitUntil(extCtx, nextRunAt, make(chan struct{}), 10*time.Second)
	assert.True(t, result) // returns immediately
}

func TestWaitUntil_NextRunAtLongerThanFallback(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	nextRunAt := tstz(time.Now().Add(10 * time.Second))
	start := time.Now()
	result := waitUntil(extCtx, nextRunAt, make(chan struct{}), 20*time.Millisecond)
	assert.True(t, result)
	assert.WithinDuration(t, start.Add(20*time.Millisecond), time.Now(), 50*time.Millisecond)
}

// ---------------------------------------------------------------------------
// getNextRunAt
// ---------------------------------------------------------------------------

func TestGetNextRunAt_ReturnsValue(t *testing.T) {
	expected := tstz(time.Now().Add(5 * time.Minute))
	svc := &SchedulerService{skdRepo: &mockSkdRepo{nextRunAt: expected}}

	got := svc.getNextRunAt(testCtx)
	assert.Equal(t, expected, got)
}

func TestGetNextRunAt_ErrorReturnsInvalid(t *testing.T) {
	svc := &SchedulerService{skdRepo: &mockSkdRepo{nextRunAtErr: errors.New("no rows")}}

	got := svc.getNextRunAt(testCtx)
	assert.False(t, got.Valid)
}

// ---------------------------------------------------------------------------
// runDueTasks
// ---------------------------------------------------------------------------

func TestRunDueTasks_NoTasks(t *testing.T) {
	repo := &mockSkdRepo{}
	bus := &mockEventBus{}
	svc := &SchedulerService{skdRepo: repo, eventBus: bus}

	svc.runDueTasks(testCtx)
	assert.Empty(t, bus.createdTaskNames)
	assert.Empty(t, repo.savedTasks)
}

func TestRunDueTasks_ClaimError_NonNoRows(t *testing.T) {
	repo := &mockSkdRepo{
		claimResults: []skd_db.ScheduledTask{{}},
		claimErrors:  []error{errors.New("db error")},
	}
	svc := &SchedulerService{skdRepo: repo, eventBus: &mockEventBus{}}

	svc.runDueTasks(testCtx) // should log and return, not panic
	assert.Empty(t, repo.savedTasks)
}

func TestRunDueTasks_OneShot_DisablesAfterFiring(t *testing.T) {
	task := skd_db.ScheduledTask{ID: "t1", EventName: "my-event", CronExpr: ""}
	repo := &mockSkdRepo{claimResults: []skd_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{skdRepo: repo, eventBus: bus}

	svc.runDueTasks(testCtx)

	assert.Equal(t, []events.EventName{"my-event"}, bus.createdTaskNames)
	assert.Len(t, repo.savedTasks, 1)
	assert.False(t, repo.savedTasks[0].RunAt.Valid, "one-shot task should be disabled")
}

func TestRunDueTasks_Recurring_ReschedulesWithNextCronTime(t *testing.T) {
	task := skd_db.ScheduledTask{ID: "t2", EventName: "cron-ev", CronExpr: "* * * * *"}
	repo := &mockSkdRepo{claimResults: []skd_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{skdRepo: repo, eventBus: bus}

	svc.runDueTasks(testCtx)

	assert.Equal(t, []events.EventName{"cron-ev"}, bus.createdTaskNames)
	assert.Len(t, repo.savedTasks, 1)
	saved := repo.savedTasks[0]
	assert.True(t, saved.RunAt.Valid)
	assert.True(t, saved.RunAt.Time.After(time.Now()))
	assert.Equal(t, skd_db.ScheduledTaskStatusPending, saved.Status)
}

func TestRunDueTasks_Recurring_InvalidCronExpr_DisablesTask(t *testing.T) {
	task := skd_db.ScheduledTask{ID: "t3", EventName: "bad", CronExpr: "not-valid"}
	repo := &mockSkdRepo{claimResults: []skd_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{skdRepo: repo, eventBus: bus}

	svc.runDueTasks(testCtx)

	assert.Len(t, repo.savedTasks, 1)
	assert.False(t, repo.savedTasks[0].RunAt.Valid)
}

func TestRunDueTasks_CreateTaskError_ReschedulesWithRetryDelay(t *testing.T) {
	task := skd_db.ScheduledTask{ID: "t4", EventName: "fail-ev"}
	repo := &mockSkdRepo{claimResults: []skd_db.ScheduledTask{task}}
	bus := &mockEventBus{createTaskErr: errors.New("bus down")}
	svc := &SchedulerService{skdRepo: repo, eventBus: bus}

	svc.runDueTasks(testCtx)

	assert.Len(t, repo.savedTasks, 1)
	saved := repo.savedTasks[0]
	assert.True(t, saved.RunAt.Valid)
	assert.True(t, saved.RunAt.Time.After(time.Now()))
	assert.Equal(t, skd_db.ScheduledTaskStatusPending, saved.Status)
}

func TestRunDueTasks_MultipleTasks_ProcessedInOrder(t *testing.T) {
	tasks := []skd_db.ScheduledTask{
		{ID: "t1", EventName: "event-a"},
		{ID: "t2", EventName: "event-b"},
	}
	repo := &mockSkdRepo{claimResults: tasks}
	bus := &mockEventBus{}
	svc := &SchedulerService{skdRepo: repo, eventBus: bus}

	svc.runDueTasks(testCtx)

	assert.Equal(t, []events.EventName{"event-a", "event-b"}, bus.createdTaskNames)
	assert.Len(t, repo.savedTasks, 2)
}

func TestRunDueTasks_ValidJsonPayload_Dispatched(t *testing.T) {
	task := skd_db.ScheduledTask{
		ID:        "t5",
		EventName: "payload-ev",
		Payload:   events.EventData{},
	}
	repo := &mockSkdRepo{claimResults: []skd_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{skdRepo: repo, eventBus: bus}

	svc.runDueTasks(testCtx)

	assert.Equal(t, []events.EventName{"payload-ev"}, bus.createdTaskNames)
}

// ---------------------------------------------------------------------------
// NewSchedulerService — channel wiring
// ---------------------------------------------------------------------------

func TestNewSchedulerService_ChannelWired(t *testing.T) {
	svc := NewSchedulerService(&mockSkdRepo{}, &mockEventBus{}, "")

	assert.NotNil(t, svc.notifyCh)
	assert.NotNil(t, svc.notify)

	svc.notifyCh <- struct{}{}
	select {
	case <-svc.notify:
		// OK — same underlying channel
	default:
		t.Fatal("notify channel is not wired to notifyCh")
	}
}
