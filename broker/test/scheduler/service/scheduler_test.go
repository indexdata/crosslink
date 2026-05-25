package sched_service

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	sched_service "github.com/indexdata/crosslink/broker/scheduler/service"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var connString string
var schedRepo sched_db.SchedRepo
var appCtx = common.CreateExtCtxWithArgs(context.Background(), nil)

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	connString = connStr
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	app.DB_PROVISION = true

	test.Expect(app.RunDbUp(), "failed to run db migrations")

	pool, err := app.InitDbPool()
	test.Expect(err, "failed to init db pool")

	schedRepo = sched_db.CreateSchedRepo(pool)

	code := m.Run()

	pool.Close()
	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// countingEventBus records dispatched tasks and is safe for concurrent use.
type countingEventBus struct {
	events.EventBus
	mu     sync.Mutex
	claims []string
}

func (b *countingEventBus) CreateTask(_ string, _ events.EventName, data events.EventData, _ events.EventDomain, _ *string, _ events.SignalTarget) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	taskId := uuid.New().String()
	b.claims = append(b.claims, data.Note)
	return taskId, nil
}

func (b *countingEventBus) totalClaims() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.claims)
}

func (b *countingEventBus) getClaims() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.claims
}

func overdueTask() sched_db.SaveScheduledTaskParams {
	id := uuid.New().String()
	return sched_db.SaveScheduledTaskParams{
		ID:        id,
		EventName: events.EventNameSendNotification,
		CronExpr:  "",
		Payload:   events.EventData{CommonEventData: events.CommonEventData{Note: id}},
		RunAt:     pgtype.Timestamptz{Time: time.Now().Add(-1 * time.Second), Valid: true},
		Status:    sched_db.ScheduledTaskStatusPending,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func startScheduler(t *testing.T, ctx context.Context, bus events.EventBus) {
	t.Helper()
	pool, err := app.InitDbPool()
	assert.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := sched_db.CreateSchedRepo(pool)
	svc := sched_service.NewSchedulerService(repo, bus, connString)
	extCtx := common.CreateExtCtxWithArgs(ctx, nil)
	assert.NoError(t, svc.Listen(extCtx))
	go svc.Run(extCtx)
}

// ---------------------------------------------------------------------------
// Multi-instance: no double processing
// ---------------------------------------------------------------------------

// TestMultipleInstances_TaskClaimedExactlyOnce verifies that two scheduler
// instances running concurrently dispatch a single overdue task exactly once.
// FOR UPDATE SKIP LOCKED prevents double claiming.
func TestMultipleInstances_TaskClaimedExactlyOnce(t *testing.T) {
	bus := &countingEventBus{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	params := overdueTask()
	_, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	startScheduler(t, ctx, bus)
	startScheduler(t, ctx, bus)

	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		return bus.totalClaims() >= 1
	}), "task was never claimed")

	time.Sleep(150 * time.Millisecond) // extra time to catch any duplicate

	assert.Equal(t, 1, bus.totalClaims(), "task must be claimed exactly once")
}

// TestMultipleInstances_EachTaskClaimedOnce verifies N tasks across M instances
// are each dispatched exactly once — no duplication, no starvation.
func TestMultipleInstances_EachTaskClaimedOnce(t *testing.T) {
	const taskCount = 5
	const instanceCount = 3

	bus := &countingEventBus{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ids := []string{}

	for i := 0; i < taskCount; i++ {
		task, err := schedRepo.SaveScheduledTask(appCtx, overdueTask())
		ids = append(ids, task.ID)
		assert.NoError(t, err)
	}

	for i := 0; i < instanceCount; i++ {
		startScheduler(t, ctx, bus)
	}

	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		return bus.totalClaims() >= taskCount
	}), "not all tasks were claimed")

	time.Sleep(150 * time.Millisecond)

	assert.Equal(t, taskCount, bus.totalClaims(), "each task must be dispatched exactly once")
	assert.ElementsMatch(t, ids, bus.getClaims(), "each task must be claimed exactly once")
}

// TestMultipleInstances_HighConcurrency runs 10 tasks across 5 instances and
// asserts each task is claimed exactly once under higher parallelism.
func TestMultipleInstances_HighConcurrency(t *testing.T) {
	const taskCount = 10
	const instanceCount = 5

	var total atomic.Int64
	bus := &countingEventBus{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < taskCount; i++ {
		_, err := schedRepo.SaveScheduledTask(appCtx, overdueTask())
		assert.NoError(t, err)
	}

	for i := 0; i < instanceCount; i++ {
		startScheduler(t, ctx, bus)
	}

	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		return bus.totalClaims() >= taskCount
	}), "not all tasks claimed")

	time.Sleep(200 * time.Millisecond)
	total.Store(int64(bus.totalClaims()))

	assert.Equal(t, int64(taskCount), total.Load(),
		"all tasks must be dispatched exactly once across all instances")
}

// ---------------------------------------------------------------------------
// LISTEN/NOTIFY wake-up
// ---------------------------------------------------------------------------

// TestListen_NotifyWakesScheduler verifies that inserting a new overdue task
// (which triggers NOTIFY internally) wakes the sleeping scheduler well within
// the fallback poll window.
func TestListen_NotifyWakesScheduler(t *testing.T) {
	bus := &countingEventBus{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startScheduler(t, ctx, bus)

	// Give the scheduler time to enter its wait state.
	time.Sleep(100 * time.Millisecond)

	// SaveScheduledTask sends NOTIFY crosslink_sched_channel internally.
	_, err := schedRepo.SaveScheduledTask(appCtx, overdueTask())
	assert.NoError(t, err)

	// Should wake within ~200ms due to NOTIFY, not wait for the 5-min fallback.
	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		return bus.totalClaims() >= 1
	}), "scheduler was not woken by NOTIFY")
}

// ---------------------------------------------------------------------------
// Reconnect after connection loss
// ---------------------------------------------------------------------------

// TestListen_ReconnectsAfterConnectionLoss terminates the scheduler's LISTEN
// connection via pg_terminate_backend and verifies the scheduler reconnects
// and continues processing new tasks afterwards.
func TestListen_ReconnectsAfterConnectionLoss(t *testing.T) {
	bus := &countingEventBus{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startScheduler(t, ctx, bus)

	// Let the scheduler reach its idle wait state.
	time.Sleep(150 * time.Millisecond)

	// Kill all LISTEN connections to simulate a network interruption.
	adminPool, err := app.InitDbPool()
	assert.NoError(t, err)
	t.Cleanup(adminPool.Close)
	killCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	_, err = adminPool.Exec(killCtx,
		`SELECT pg_terminate_backend(pid)
         FROM pg_stat_activity
         WHERE query LIKE $1
           AND pid <> pg_backend_pid()`,
		"LISTEN%")
	assert.NoError(t, err)

	// Allow time for reconnect (exponential backoff starts at 1 s).
	time.Sleep(2 * time.Second)

	// After reconnect the scheduler must still pick up newly inserted tasks.
	_, err = schedRepo.SaveScheduledTask(appCtx, overdueTask())
	assert.NoError(t, err)

	assert.True(t, test.WaitForPredicateToBeTrue(func() bool {
		return bus.totalClaims() >= 1
	}), "scheduler did not recover after connection loss")
}

// ---------------------------------------------------------------------------
// Context cancellation
// ---------------------------------------------------------------------------

// TestScheduler_StopsOnContextCancel verifies that cancelling the context
// causes the Run() loop to exit cleanly within a reasonable time.
func TestScheduler_StopsOnContextCancel(t *testing.T) {
	bus := &countingEventBus{}
	ctx, cancel := context.WithCancel(context.Background())

	pool, err := app.InitDbPool()
	assert.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := sched_db.CreateSchedRepo(pool)
	svc := sched_service.NewSchedulerService(repo, bus, connString)
	extCtx := common.CreateExtCtxWithArgs(ctx, nil)
	assert.NoError(t, svc.Listen(extCtx))

	stopped := make(chan struct{})
	go func() {
		svc.Run(extCtx)
		close(stopped)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-stopped:
		// scheduler loop exited cleanly
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not stop after context cancellation")
	}
}
