package sched_service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
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
// Mock SchedRepo
// ---------------------------------------------------------------------------

type mockSchedRepo struct {
	sched_db.PgSchedRepo
	claimResults  []sched_db.ScheduledTask
	claimErrors   []error
	claimIndex    int
	savedTasks    []sched_db.SaveScheduledTaskParams
	saveError     error
	nextRunAt     pgtype.Timestamptz
	nextRunAtErr  error
	stuckTasks    []sched_db.ScheduledTask
	stuckTasksErr error
	stuckAfter    time.Duration
}

func (m *mockSchedRepo) WithTxFunc(ctx common.ExtendedContext, fn func(sched_db.SchedRepo) error) error {
	return fn(m)
}

func (m *mockSchedRepo) ClaimNextScheduledTask(_ common.ExtendedContext) (sched_db.ScheduledTask, error) {
	if m.claimIndex >= len(m.claimResults) {
		return sched_db.ScheduledTask{}, pgx.ErrNoRows
	}
	task := m.claimResults[m.claimIndex]
	var err error
	if m.claimIndex < len(m.claimErrors) {
		err = m.claimErrors[m.claimIndex]
	}
	m.claimIndex++
	return task, err
}

func (m *mockSchedRepo) SaveScheduledTask(_ common.ExtendedContext, p sched_db.SaveScheduledTaskParams) (sched_db.ScheduledTask, error) {
	m.savedTasks = append(m.savedTasks, p)
	return sched_db.ScheduledTask(p), m.saveError
}

func (m *mockSchedRepo) GetNextRunAt(_ common.ExtendedContext) (pgtype.Timestamptz, error) {
	return m.nextRunAt, m.nextRunAtErr
}

func (m *mockSchedRepo) GetStuckRunningTasks(_ common.ExtendedContext, stuckAfter time.Duration) ([]sched_db.ScheduledTask, error) {
	m.stuckAfter = stuckAfter
	return m.stuckTasks, m.stuckTasksErr
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
// NextScheduleTime
// ---------------------------------------------------------------------------

func TestNextScheduleTime_ValidExpr(t *testing.T) {
	ts, err := NextScheduleTime("FREQ=MINUTELY") // every minute
	assert.NoError(t, err)
	assert.True(t, ts.Valid)
	assert.True(t, ts.Time.After(time.Now()))
}

func TestNextScheduleTime_InvalidExpr(t *testing.T) {
	_, err := NextScheduleTime("not-a-rrule")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid rrule string")
}

func TestNextScheduleTime_SpecificSchedule(t *testing.T) {
	// "FREQ=WEEKLY;BYDAY=MO;BYHOUR=9;BYMINUTE=0" = every Monday at 09:00 — just verify it's in the future
	ts, err := NextScheduleTime("FREQ=WEEKLY;BYDAY=MO;BYHOUR=9;BYMINUTE=0")
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

	result := waitUntil(extCtx, invalidTstz(), make(chan struct{}), 10*time.Second, false)
	assert.False(t, result, "should return false when context is cancelled")
}

func TestWaitUntil_NotifyWakes(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	ch := make(chan struct{}, 1)
	ch <- struct{}{} // pre-signal

	result := waitUntil(extCtx, invalidTstz(), ch, 10*time.Second, false)
	assert.True(t, result)
}

func TestWaitUntil_FallbackElapsed(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	start := time.Now()
	result := waitUntil(extCtx, invalidTstz(), make(chan struct{}), 20*time.Millisecond, false)
	assert.True(t, result)
	assert.WithinDuration(t, start.Add(20*time.Millisecond), time.Now(), 50*time.Millisecond)
}

func TestWaitUntil_NextRunAtSooner(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	nextRunAt := tstz(time.Now().Add(20 * time.Millisecond))
	start := time.Now()
	result := waitUntil(extCtx, nextRunAt, make(chan struct{}), 10*time.Second, false)
	assert.True(t, result)
	assert.WithinDuration(t, start.Add(20*time.Millisecond), time.Now(), 50*time.Millisecond)
}

// TestWaitUntil_NextRunAtAlreadyOverdue_MadeProgress verifies that an overdue
// timestamp returns immediately when the caller made progress (claimed a task).
func TestWaitUntil_NextRunAtAlreadyOverdue_MadeProgress(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	nextRunAt := tstz(time.Now().Add(-1 * time.Second))
	result := waitUntil(extCtx, nextRunAt, make(chan struct{}), 10*time.Second, true)
	assert.True(t, result) // returns immediately
}

// TestWaitUntil_NextRunAtAlreadyOverdue_NoProgress verifies that an overdue
// timestamp does NOT return immediately when no progress was made, to prevent
// a tight spin loop on persistent claim errors.
func TestWaitUntil_NextRunAtAlreadyOverdue_NoProgress(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	nextRunAt := tstz(time.Now().Add(-1 * time.Second))
	start := time.Now()
	result := waitUntil(extCtx, nextRunAt, make(chan struct{}), 20*time.Millisecond, false)
	assert.True(t, result)
	// Must have slept the fallback, not returned immediately.
	assert.WithinDuration(t, start.Add(20*time.Millisecond), time.Now(), 50*time.Millisecond)
}

func TestWaitUntil_NextRunAtLongerThanFallback(t *testing.T) {
	extCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	nextRunAt := tstz(time.Now().Add(10 * time.Second))
	start := time.Now()
	result := waitUntil(extCtx, nextRunAt, make(chan struct{}), 20*time.Millisecond, false)
	assert.True(t, result)
	assert.WithinDuration(t, start.Add(20*time.Millisecond), time.Now(), 50*time.Millisecond)
}

// ---------------------------------------------------------------------------
// getNextRunAt
// ---------------------------------------------------------------------------

func TestGetNextRunAt_ReturnsValue(t *testing.T) {
	expected := tstz(time.Now().Add(5 * time.Minute))
	svc := &SchedulerService{schedRepo: &mockSchedRepo{nextRunAt: expected}}

	got := svc.getNextRunAt(testCtx)
	assert.Equal(t, expected, got)
}

func TestGetNextRunAt_ErrorReturnsInvalid(t *testing.T) {
	svc := &SchedulerService{schedRepo: &mockSchedRepo{nextRunAtErr: errors.New("no rows")}}

	got := svc.getNextRunAt(testCtx)
	assert.False(t, got.Valid)
}

// ---------------------------------------------------------------------------
// runDueTasks
// ---------------------------------------------------------------------------

func TestRunDueTasks_NoTasks(t *testing.T) {
	repo := &mockSchedRepo{}
	bus := &mockEventBus{}
	svc := &SchedulerService{schedRepo: repo, eventBus: bus}

	progress := svc.runDueTasks(testCtx)
	assert.False(t, progress)
	assert.Empty(t, bus.createdTaskNames)
	assert.Empty(t, repo.savedTasks)
}

func TestRunDueTasks_ClaimError_NonNoRows(t *testing.T) {
	repo := &mockSchedRepo{
		claimResults: []sched_db.ScheduledTask{{}},
		claimErrors:  []error{errors.New("db error")},
	}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	progress := svc.runDueTasks(testCtx)
	assert.False(t, progress)
	assert.Empty(t, repo.savedTasks)
}

func TestRunDueTasks_OneShot_DisablesAfterFiring(t *testing.T) {
	task := sched_db.ScheduledTask{ID: "t1", EventName: "my-event", Schedule: "", RunAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}}
	repo := &mockSchedRepo{claimResults: []sched_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{schedRepo: repo, eventBus: bus}

	progress := svc.runDueTasks(testCtx)

	assert.True(t, progress)
	assert.Equal(t, []events.EventName{"my-event"}, bus.createdTaskNames)
	assert.Len(t, repo.savedTasks, 1)
	assert.False(t, repo.savedTasks[0].RunAt.Valid, "one-shot task should be disabled")
}

func TestRunDueTasks_Recurring_ReschedulesWithNextScheduleTime(t *testing.T) {
	task := sched_db.ScheduledTask{ID: "t2", EventName: "rrule-ev", Schedule: "FREQ=MINUTELY"}
	repo := &mockSchedRepo{claimResults: []sched_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{schedRepo: repo, eventBus: bus}

	progress := svc.runDueTasks(testCtx)

	assert.True(t, progress)
	assert.Equal(t, []events.EventName{"rrule-ev"}, bus.createdTaskNames)
	assert.Len(t, repo.savedTasks, 1)
	saved := repo.savedTasks[0]
	assert.True(t, saved.RunAt.Valid)
	assert.True(t, saved.RunAt.Time.After(time.Now()))
	assert.Equal(t, sched_db.ScheduledTaskStatusPending, saved.Status)
}

func TestRunDueTasks_Recurring_InvalidSchedule_DisablesTask(t *testing.T) {
	task := sched_db.ScheduledTask{ID: "t3", EventName: "bad", Schedule: "not-valid"}
	repo := &mockSchedRepo{claimResults: []sched_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{schedRepo: repo, eventBus: bus}

	progress := svc.runDueTasks(testCtx)

	assert.True(t, progress)
	assert.Len(t, repo.savedTasks, 1)
	assert.False(t, repo.savedTasks[0].RunAt.Valid)
}

func TestRunDueTasks_CreateTaskError_RollsBack(t *testing.T) {
	task := sched_db.ScheduledTask{ID: "t4", EventName: "fail-ev"}
	repo := &mockSchedRepo{claimResults: []sched_db.ScheduledTask{task}}
	bus := &mockEventBus{createTaskErr: errors.New("bus down")}
	svc := &SchedulerService{schedRepo: repo, eventBus: bus}

	progress := svc.runDueTasks(testCtx)

	// Transaction rolled back → no explicit reschedule, task stays pending via rollback.
	assert.False(t, progress)
	assert.Empty(t, repo.savedTasks)
}

func TestRunDueTasks_MultipleTasks_ProcessedInOrder(t *testing.T) {
	tasks := []sched_db.ScheduledTask{
		{ID: "t1", EventName: "event-a"},
		{ID: "t2", EventName: "event-b"},
	}
	repo := &mockSchedRepo{claimResults: tasks}
	bus := &mockEventBus{}
	svc := &SchedulerService{schedRepo: repo, eventBus: bus}

	progress := svc.runDueTasks(testCtx)

	assert.True(t, progress)
	assert.Equal(t, []events.EventName{"event-a", "event-b"}, bus.createdTaskNames)
	assert.Len(t, repo.savedTasks, 2)
}

func TestRunDueTasks_ValidJsonPayload_Dispatched(t *testing.T) {
	task := sched_db.ScheduledTask{
		ID:        "t5",
		EventName: "payload-ev",
		Payload:   events.EventData{},
	}
	repo := &mockSchedRepo{claimResults: []sched_db.ScheduledTask{task}}
	bus := &mockEventBus{}
	svc := &SchedulerService{schedRepo: repo, eventBus: bus}

	progress := svc.runDueTasks(testCtx)

	assert.True(t, progress)
	assert.Equal(t, []events.EventName{"payload-ev"}, bus.createdTaskNames)
}

// ---------------------------------------------------------------------------
// rescheduleLongRunningTasks
// ---------------------------------------------------------------------------

// TestRescheduleLongRunning_NoStuckTasks verifies that nothing is saved when
// there are no stuck tasks.
func TestRescheduleLongRunning_NoStuckTasks(t *testing.T) {
	repo := &mockSchedRepo{stuckTasks: nil}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	svc.rescheduleLongRunningTasks(testCtx)

	assert.Empty(t, repo.savedTasks)
}

// TestRescheduleLongRunning_RepoError_DoesNotSave verifies that a repo error
// is handled gracefully without saving anything.
func TestRescheduleLongRunning_RepoError_DoesNotSave(t *testing.T) {
	repo := &mockSchedRepo{stuckTasksErr: errors.New("db error")}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	svc.rescheduleLongRunningTasks(testCtx)

	assert.Empty(t, repo.savedTasks)
}

// TestRescheduleLongRunning_OneShot_ReschedulesWithRetryDelay verifies that a
// stuck one-shot task (no rrule) is reset to pending with run_at = now + retry.
func TestRescheduleLongRunning_OneShot_ReschedulesWithRetryDelay(t *testing.T) {
	stuck := sched_db.ScheduledTask{
		ID:        "stuck-1",
		EventName: "one-shot",
		Schedule:  "",
		Status:    sched_db.ScheduledTaskStatusRunning,
	}
	repo := &mockSchedRepo{stuckTasks: []sched_db.ScheduledTask{stuck}}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	before := time.Now()
	svc.rescheduleLongRunningTasks(testCtx)
	after := time.Now()

	assert.Len(t, repo.savedTasks, 1)
	saved := repo.savedTasks[0]
	assert.Equal(t, sched_db.ScheduledTaskStatusPending, saved.Status)
	assert.True(t, saved.RunAt.Valid)
	assert.True(t, saved.RunAt.Time.After(before))
	assert.True(t, saved.RunAt.Time.After(after)) // run_at is in the future
}

// TestRescheduleLongRunning_ProcessesRepoReturnedTaskRegardlessOfUpdatedAt
// verifies that GetStuckRunningTasks is the sole age filter for stuck tasks.
func TestRescheduleLongRunning_ProcessesRepoReturnedTaskRegardlessOfUpdatedAt(t *testing.T) {
	stuck := sched_db.ScheduledTask{
		ID:        "stuck-recent",
		EventName: "one-shot",
		Schedule:  "",
		Status:    sched_db.ScheduledTaskStatusRunning,
		UpdatedAt: tstz(time.Now()),
	}
	repo := &mockSchedRepo{stuckTasks: []sched_db.ScheduledTask{stuck}}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	svc.rescheduleLongRunningTasks(testCtx)

	assert.Equal(t, time.Hour, repo.stuckAfter)
	assert.Len(t, repo.savedTasks, 1)
	assert.Equal(t, sched_db.ScheduledTaskStatusPending, repo.savedTasks[0].Status)
	assert.True(t, repo.savedTasks[0].RunAt.Valid)
}

// TestRescheduleLongRunning_Recurring_ReschedulesWithNextScheduleTime verifies that
// a stuck recurring task is reset to pending with the next schedule-computed run_at.
func TestRescheduleLongRunning_Recurring_ReschedulesWithNextScheduleTime(t *testing.T) {
	stuck := sched_db.ScheduledTask{
		ID:        "stuck-2",
		EventName: "rrule-ev",
		Schedule:  "FREQ=MINUTELY", // every minute
		Status:    sched_db.ScheduledTaskStatusRunning,
	}
	repo := &mockSchedRepo{stuckTasks: []sched_db.ScheduledTask{stuck}}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	svc.rescheduleLongRunningTasks(testCtx)

	assert.Len(t, repo.savedTasks, 1)
	saved := repo.savedTasks[0]
	assert.Equal(t, sched_db.ScheduledTaskStatusPending, saved.Status)
	assert.True(t, saved.RunAt.Valid)
	assert.True(t, saved.RunAt.Time.After(time.Now()))
}

// TestRescheduleLongRunning_InvalidRrule_DisablesTask verifies that a stuck task
// with an invalid rrule expression is disabled rather than rescheduled.
func TestRescheduleLongRunning_InvalidRrule_DisablesTask(t *testing.T) {
	stuck := sched_db.ScheduledTask{
		ID:        "stuck-3",
		EventName: "bad-rrule",
		Schedule:  "not-a-rrule",
		Status:    sched_db.ScheduledTaskStatusRunning,
	}
	repo := &mockSchedRepo{stuckTasks: []sched_db.ScheduledTask{stuck}}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	svc.rescheduleLongRunningTasks(testCtx)

	assert.Len(t, repo.savedTasks, 1)
	saved := repo.savedTasks[0]
	assert.Equal(t, sched_db.ScheduledTaskStatusStopped, saved.Status)
	assert.False(t, saved.RunAt.Valid)
}

// TestRescheduleLongRunning_MultipleStuck_AllRescheduled verifies that all
// stuck tasks in the result set are processed.
func TestRescheduleLongRunning_MultipleStuck_AllRescheduled(t *testing.T) {
	stuckTasks := []sched_db.ScheduledTask{
		{ID: "s1", EventName: "ev-a", Schedule: "", Status: sched_db.ScheduledTaskStatusRunning},
		{ID: "s2", EventName: "ev-b", Schedule: "FREQ=MINUTELY", Status: sched_db.ScheduledTaskStatusRunning},
	}
	repo := &mockSchedRepo{stuckTasks: stuckTasks}
	svc := &SchedulerService{schedRepo: repo, eventBus: &mockEventBus{}}

	svc.rescheduleLongRunningTasks(testCtx)

	assert.Len(t, repo.savedTasks, 2)
	for _, saved := range repo.savedTasks {
		assert.Equal(t, sched_db.ScheduledTaskStatusPending, saved.Status)
		assert.True(t, saved.RunAt.Valid)
	}
}

// ---------------------------------------------------------------------------
// NewSchedulerService — channel wiring
// ---------------------------------------------------------------------------

func TestNewSchedulerService_ChannelWired(t *testing.T) {
	svc := NewSchedulerService(&mockSchedRepo{}, &mockEventBus{}, "")

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
