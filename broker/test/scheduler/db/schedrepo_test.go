package sched_db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

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
	test.Expect(test.TerminatePGContainer(ctx, pgContainer), "failed to stop db container")
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
func newTask(schedule string, runAt pgtype.Timestamptz) sched_db.SaveScheduledTaskParams {
	return sched_db.SaveScheduledTaskParams{
		ID:        uuid.NewString(),
		EventName: events.EventNameSendNotification,
		Schedule:  schedule,
		RunAt:     runAt,
		Status:    sched_db.ScheduledTaskStatusPending,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func tstz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func stopTask(t *testing.T, task sched_db.ScheduledTask) {
	task.Status = sched_db.ScheduledTaskStatusStopped
	_, err := schedRepo.SaveScheduledTask(appCtx, sched_db.SaveScheduledTaskParams(task))
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// SaveScheduledTask
// ---------------------------------------------------------------------------

func TestSaveScheduledTask_Insert(t *testing.T) {
	params := newTask("FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0", tstz(time.Now().Add(1*time.Minute)))

	saved, err := schedRepo.SaveScheduledTask(appCtx, params)

	assert.NoError(t, err)
	assert.Equal(t, params.ID, saved.ID)
	assert.Equal(t, params.EventName, saved.EventName)
	assert.Equal(t, params.Schedule, saved.Schedule)
	assert.Equal(t, sched_db.ScheduledTaskStatusPending, saved.Status)
	assert.True(t, saved.CreatedAt.Valid)
	assert.True(t, saved.UpdatedAt.Valid)
	assert.Equal(t, saved.CreatedAt.Time, saved.UpdatedAt.Time)

	stopTask(t, saved)
}

func TestSaveScheduledTask_Upsert_UpdatesFields(t *testing.T) {
	params := newTask("FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0", tstz(time.Now().Add(1*time.Hour)))
	_, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	params.Schedule = "FREQ=WEEKLY;BYDAY=MO;BYHOUR=7;BYMINUTE=0"
	params.RunAt = tstz(time.Now().Add(2 * time.Hour))

	updated, err := schedRepo.SaveScheduledTask(appCtx, params)

	assert.NoError(t, err)
	assert.Equal(t, params.ID, updated.ID)
	assert.Equal(t, "FREQ=WEEKLY;BYDAY=MO;BYHOUR=7;BYMINUTE=0", updated.Schedule)

	stopTask(t, updated)
}

func TestSaveScheduledTask_WithPayload(t *testing.T) {
	params := newTask("", tstz(time.Now().Add(1*time.Minute)))
	params.ActionData = events.EventData{
		CommonEventData: events.CommonEventData{Note: "hello from scheduler"},
	}

	saved, err := schedRepo.SaveScheduledTask(appCtx, params)

	assert.NoError(t, err)
	assert.Equal(t, "hello from scheduler", saved.ActionData.Note)

	stopTask(t, saved)
}

func TestScheduledTaskOwnerScopes(t *testing.T) {
	ownerA := "ISIL:SCOPE-A-" + uuid.NewString()
	ownerB := "ISIL:SCOPE-B-" + uuid.NewString()
	taskAParams := newTask("", tstz(time.Now().Add(time.Hour)))
	taskAParams.Owner = ownerA
	taskBParams := newTask("", tstz(time.Now().Add(time.Hour)))
	taskBParams.Owner = ownerB
	taskA, err := schedRepo.SaveScheduledTask(appCtx, taskAParams)
	assert.NoError(t, err)
	taskB, err := schedRepo.SaveScheduledTask(appCtx, taskBParams)
	assert.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, schedRepo.DeleteScheduledTask(appCtx, taskA.ID, nil))
		assert.NoError(t, schedRepo.DeleteScheduledTask(appCtx, taskB.ID, nil))
	})

	allTasks, _, err := schedRepo.GetScheduledTasks(appCtx, sched_db.GetScheduledTasksParams{Limit: 1000})
	assert.NoError(t, err)
	allIDs := make([]string, 0, len(allTasks))
	for _, task := range allTasks {
		allIDs = append(allIDs, task.ID)
	}
	assert.Contains(t, allIDs, taskA.ID)
	assert.Contains(t, allIDs, taskB.ID)

	ownerATasks, count, err := schedRepo.GetScheduledTasks(appCtx, sched_db.GetScheduledTasksParams{
		Owners: []string{ownerA}, Limit: 100,
	})
	assert.NoError(t, err)
	assert.Equal(t, int64(1), count)
	if assert.Len(t, ownerATasks, 1) {
		assert.Equal(t, taskA.ID, ownerATasks[0].ID)
	}

	found, err := schedRepo.GetScheduledTaskById(appCtx, taskB.ID, nil)
	assert.NoError(t, err)
	assert.Equal(t, taskB.ID, found.ID)
	_, err = schedRepo.GetScheduledTaskById(appCtx, taskB.ID, []string{ownerA})
	assert.ErrorIs(t, err, pgx.ErrNoRows)

	emptyScope := []string{}
	emptyTasks, count, err := schedRepo.GetScheduledTasks(appCtx, sched_db.GetScheduledTasksParams{
		Owners: emptyScope, Limit: 100,
	})
	assert.NoError(t, err)
	assert.Zero(t, count)
	assert.Empty(t, emptyTasks)
	_, err = schedRepo.GetScheduledTaskById(appCtx, taskB.ID, emptyScope)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.NoError(t, schedRepo.DeleteScheduledTask(appCtx, taskB.ID, emptyScope))
	found, err = schedRepo.GetScheduledTaskById(appCtx, taskB.ID, nil)
	assert.NoError(t, err)
	assert.Equal(t, taskB.ID, found.ID)
}

// ---------------------------------------------------------------------------
// GetNextRunAt
// ---------------------------------------------------------------------------

func TestGetNextRunAt_ReturnsPendingTask(t *testing.T) {
	params := newTask("FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0", tstz(time.Now().Add(5*time.Minute)))
	saved, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	next, err := schedRepo.GetNextRunAt(appCtx)

	assert.NoError(t, err)
	assert.True(t, next.Valid)
	assert.True(t, next.Time.After(time.Now()))

	stopTask(t, saved)
}

func TestGetNextRunAt_ReturnsEarliestPendingTask(t *testing.T) {
	earlier := tstz(time.Now().Add(2 * time.Minute))
	later := tstz(time.Now().Add(4 * time.Hour))

	p1 := newTask("", earlier)
	p2 := newTask("", later)

	s1, err := schedRepo.SaveScheduledTask(appCtx, p1)
	assert.NoError(t, err)
	s2, err := schedRepo.SaveScheduledTask(appCtx, p2)
	assert.NoError(t, err)

	next, err := schedRepo.GetNextRunAt(appCtx)

	assert.NoError(t, err)
	assert.True(t, next.Valid)
	assert.WithinDuration(t, earlier.Time, next.Time, time.Second)
	assert.True(t, next.Time.Before(later.Time), "returned run_at should be the earlier of the two tasks")

	stopTask(t, s1)
	stopTask(t, s2)
}

// ---------------------------------------------------------------------------
// ClaimNextScheduledTask
// ---------------------------------------------------------------------------

func TestClaimNextScheduledTask_OverdueTask_ClaimedAndSetToRunning(t *testing.T) {
	overdue := newTask("", tstz(time.Now().Add(-1*time.Second)))
	_, err := schedRepo.SaveScheduledTask(appCtx, overdue)
	assert.NoError(t, err)

	claimed, err := schedRepo.ClaimNextScheduledTask(appCtx)

	assert.NoError(t, err)
	assert.Equal(t, sched_db.ScheduledTaskStatusRunning, claimed.Status)
	assert.True(t, claimed.UpdatedAt.Valid)

	stopTask(t, claimed)
}

func TestClaimNextScheduledTask_SetsStatusToRunning(t *testing.T) {
	params := newTask("FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0", tstz(time.Now().Add(-30*time.Second)))
	_, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	claimed, err := schedRepo.ClaimNextScheduledTask(appCtx)

	assert.NoError(t, err)
	assert.Equal(t, sched_db.ScheduledTaskStatusRunning, claimed.Status)

	stopTask(t, claimed)
}

func TestClaimNextScheduledTask_FutureTask_NotClaimed(t *testing.T) {
	i := 0
	for ; i < 100; i++ {
		claimed, err := schedRepo.ClaimNextScheduledTask(appCtx)
		if err != nil {
			assert.ErrorIs(t, err, pgx.ErrNoRows)
			break
		}
		stopTask(t, claimed)
	}
	assert.True(t, i < 100, "too many claimed tasks")

	params := newTask("", tstz(time.Now().Add(24*time.Hour)))
	saved, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	_, err = schedRepo.ClaimNextScheduledTask(appCtx)
	assert.ErrorIs(t, err, pgx.ErrNoRows)

	stopTask(t, saved)
}

// ---------------------------------------------------------------------------
// Reschedule flow (claim → save pending with updated run_at)
// ---------------------------------------------------------------------------

func TestRescheduleAfterClaim(t *testing.T) {
	params := newTask("FREQ=WEEKLY;BYDAY=MO;BYHOUR=6;BYMINUTE=0", tstz(time.Now().Add(-1*time.Second)))
	_, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	claimed, err := schedRepo.ClaimNextScheduledTask(appCtx)
	assert.NoError(t, err)
	assert.Equal(t, sched_db.ScheduledTaskStatusRunning, claimed.Status)

	claimed.Status = sched_db.ScheduledTaskStatusPending
	claimed.RunAt = tstz(time.Now().Add(5 * time.Minute))
	rescheduled, err := schedRepo.SaveScheduledTask(appCtx, sched_db.SaveScheduledTaskParams(claimed))

	assert.NoError(t, err)
	assert.Equal(t, sched_db.ScheduledTaskStatusPending, rescheduled.Status)
	assert.True(t, rescheduled.RunAt.Time.After(time.Now()))

	stopTask(t, rescheduled)
}

// ---------------------------------------------------------------------------
// GetStuckRunningTasks
// ---------------------------------------------------------------------------

// insertRunning inserts a task directly in 'running' status with the given
// updated_at so we can simulate a task that has been stuck for a known duration.
func insertRunning(t *testing.T, updatedAt time.Time) sched_db.ScheduledTask {
	t.Helper()
	params := newTask("", tstz(time.Now().Add(-10*time.Second)))
	params.Status = sched_db.ScheduledTaskStatusRunning
	params.UpdatedAt = pgtype.Timestamptz{Time: updatedAt, Valid: true}
	saved, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)
	return saved
}

func TestGetStuckRunningTasks_ReturnsTaskStuckLongerThanThreshold(t *testing.T) {
	// Insert a task that has been running for 2 hours.
	stuck := insertRunning(t, time.Now().Add(-2*time.Hour))

	tasks, err := schedRepo.GetStuckRunningTasks(appCtx, 1*time.Hour)

	assert.NoError(t, err)
	ids := make([]string, len(tasks))
	for i, tk := range tasks {
		ids[i] = tk.ID
	}
	assert.Contains(t, ids, stuck.ID)

	stopTask(t, stuck)
}

func TestGetStuckRunningTasks_DoesNotReturnRecentTask(t *testing.T) {
	// Insert a task that has been running for only 10 seconds — well within threshold.
	recent := insertRunning(t, time.Now().Add(-10*time.Second))

	tasks, err := schedRepo.GetStuckRunningTasks(appCtx, 1*time.Hour)

	assert.NoError(t, err)
	for _, tk := range tasks {
		assert.NotEqual(t, recent.ID, tk.ID, "recently started task should not be returned as stuck")
	}

	stopTask(t, recent)
}

func TestGetStuckRunningTasks_DoesNotReturnPendingTask(t *testing.T) {
	// A pending task (not running) should never appear.
	params := newTask("", tstz(time.Now().Add(-2*time.Hour)))
	saved, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	tasks, err := schedRepo.GetStuckRunningTasks(appCtx, 1*time.Hour)

	assert.NoError(t, err)
	for _, tk := range tasks {
		assert.NotEqual(t, saved.ID, tk.ID, "pending task should not appear in stuck results")
	}

	stopTask(t, saved)
}

func TestGetStuckRunningTasks_MultipleStuckTasks_AllReturned(t *testing.T) {
	stuck1 := insertRunning(t, time.Now().Add(-3*time.Hour))
	stuck2 := insertRunning(t, time.Now().Add(-2*time.Hour))

	tasks, err := schedRepo.GetStuckRunningTasks(appCtx, 1*time.Hour)

	assert.NoError(t, err)
	ids := make(map[string]bool, len(tasks))
	for _, tk := range tasks {
		ids[tk.ID] = true
	}
	assert.True(t, ids[stuck1.ID], "stuck1 should be returned")
	assert.True(t, ids[stuck2.ID], "stuck2 should be returned")

	stopTask(t, stuck1)
	stopTask(t, stuck2)
}

// ---------------------------------------------------------------------------
// Disable flow (save with invalid RunAt)
// ---------------------------------------------------------------------------

func TestDisableTask_InvalidRunAt(t *testing.T) {
	params := newTask("", tstz(time.Now().Add(1*time.Minute)))
	saved, err := schedRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	saved.RunAt = pgtype.Timestamptz{Valid: false}
	disabled, err := schedRepo.SaveScheduledTask(appCtx, sched_db.SaveScheduledTaskParams(saved))

	assert.NoError(t, err)
	assert.False(t, disabled.RunAt.Valid)

	stopTask(t, disabled)
}
