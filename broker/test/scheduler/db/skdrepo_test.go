package skd_db

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	skd_db "github.com/indexdata/crosslink/broker/scheduler/db"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var skdRepo skd_db.SkdRepo
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

	skdRepo = skd_db.CreateSkdRepo(pool)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTask(cronExpr string, runAt pgtype.Timestamptz) skd_db.SaveScheduledTaskParams {
	return skd_db.SaveScheduledTaskParams{
		ID:        uuid.NewString(),
		EventName: events.EventNameSendNotification,
		CronExpr:  cronExpr,
		RunAt:     runAt,
		Status:    skd_db.ScheduledTaskStatusPending,
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
}

func tstz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// ---------------------------------------------------------------------------
// SaveScheduledTask
// ---------------------------------------------------------------------------

func TestSaveScheduledTask_Insert(t *testing.T) {
	params := newTask("* * * * *", tstz(time.Now().Add(1*time.Minute)))

	saved, err := skdRepo.SaveScheduledTask(appCtx, params)

	assert.NoError(t, err)
	assert.Equal(t, params.ID, saved.ID)
	assert.Equal(t, params.EventName, saved.EventName)
	assert.Equal(t, params.CronExpr, saved.CronExpr)
	assert.Equal(t, skd_db.ScheduledTaskStatusPending, saved.Status)
	assert.True(t, saved.CreatedAt.Valid)
}

func TestSaveScheduledTask_Upsert_UpdatesFields(t *testing.T) {
	params := newTask("0 * * * *", tstz(time.Now().Add(1*time.Hour)))
	_, err := skdRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	params.CronExpr = "0 9 * * 1"
	params.RunAt = tstz(time.Now().Add(2 * time.Hour))

	updated, err := skdRepo.SaveScheduledTask(appCtx, params)

	assert.NoError(t, err)
	assert.Equal(t, params.ID, updated.ID)
	assert.Equal(t, "0 9 * * 1", updated.CronExpr)
}

func TestSaveScheduledTask_WithPayload(t *testing.T) {
	params := newTask("", tstz(time.Now().Add(1*time.Minute)))
	params.Payload = events.EventData{
		CommonEventData: events.CommonEventData{Note: "hello from scheduler"},
	}

	saved, err := skdRepo.SaveScheduledTask(appCtx, params)

	assert.NoError(t, err)
	assert.Equal(t, "hello from scheduler", saved.Payload.Note)
}

// ---------------------------------------------------------------------------
// GetNextRunAt
// ---------------------------------------------------------------------------

func TestGetNextRunAt_ReturnsPendingTask(t *testing.T) {
	params := newTask("* * * * *", tstz(time.Now().Add(5*time.Minute)))
	_, err := skdRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	next, err := skdRepo.GetNextRunAt(appCtx)

	assert.NoError(t, err)
	assert.True(t, next.Valid)
	assert.True(t, next.Time.After(time.Now()))
}

func TestGetNextRunAt_ReturnsEarliestPendingTask(t *testing.T) {
	earlier := tstz(time.Now().Add(2 * time.Minute))
	later := tstz(time.Now().Add(4 * time.Hour))

	p1 := newTask("", earlier)
	p2 := newTask("", later)

	_, err := skdRepo.SaveScheduledTask(appCtx, p1)
	assert.NoError(t, err)
	_, err = skdRepo.SaveScheduledTask(appCtx, p2)
	assert.NoError(t, err)

	next, err := skdRepo.GetNextRunAt(appCtx)

	assert.NoError(t, err)
	assert.True(t, next.Valid)
	// Earliest must be <= the later one
	assert.True(t, !next.Time.After(later.Time))
}

// ---------------------------------------------------------------------------
// ClaimNextScheduledTask
// ---------------------------------------------------------------------------

func TestClaimNextScheduledTask_OverdueTask_ClaimedAndSetToRunning(t *testing.T) {
	overdue := newTask("", tstz(time.Now().Add(-1*time.Second)))
	_, err := skdRepo.SaveScheduledTask(appCtx, overdue)
	assert.NoError(t, err)

	claimed, err := skdRepo.ClaimNextScheduledTask(appCtx)

	assert.NoError(t, err)
	assert.Equal(t, skd_db.ScheduledTaskStatusRunning, claimed.Status)
	assert.True(t, claimed.UpdatedAt.Valid)
}

func TestClaimNextScheduledTask_SetsStatusToRunning(t *testing.T) {
	params := newTask("* * * * *", tstz(time.Now().Add(-30*time.Second)))
	_, err := skdRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	claimed, err := skdRepo.ClaimNextScheduledTask(appCtx)

	assert.NoError(t, err)
	assert.Equal(t, skd_db.ScheduledTaskStatusRunning, claimed.Status)
}

func TestClaimNextScheduledTask_FutureTask_NotClaimed(t *testing.T) {
	// Drain any due tasks first so only future ones remain.
	for {
		_, err := skdRepo.ClaimNextScheduledTask(appCtx)
		if err != nil {
			break
		}
	}

	params := newTask("", tstz(time.Now().Add(24*time.Hour)))
	_, err := skdRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	_, err = skdRepo.ClaimNextScheduledTask(appCtx)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

// ---------------------------------------------------------------------------
// Reschedule flow (claim → save pending with updated run_at)
// ---------------------------------------------------------------------------

func TestRescheduleAfterClaim(t *testing.T) {
	params := newTask("* * * * *", tstz(time.Now().Add(-1*time.Second)))
	_, err := skdRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	claimed, err := skdRepo.ClaimNextScheduledTask(appCtx)
	assert.NoError(t, err)
	assert.Equal(t, skd_db.ScheduledTaskStatusRunning, claimed.Status)

	// Reschedule: status=pending, future run_at
	claimed.Status = skd_db.ScheduledTaskStatusPending
	claimed.RunAt = tstz(time.Now().Add(5 * time.Minute))
	rescheduled, err := skdRepo.SaveScheduledTask(appCtx, skd_db.SaveScheduledTaskParams(claimed))

	assert.NoError(t, err)
	assert.Equal(t, skd_db.ScheduledTaskStatusPending, rescheduled.Status)
	assert.True(t, rescheduled.RunAt.Time.After(time.Now()))
}

// ---------------------------------------------------------------------------
// Disable flow (save with invalid RunAt)
// ---------------------------------------------------------------------------

func TestDisableTask_InvalidRunAt(t *testing.T) {
	params := newTask("", tstz(time.Now().Add(1*time.Minute)))
	saved, err := skdRepo.SaveScheduledTask(appCtx, params)
	assert.NoError(t, err)

	saved.RunAt = pgtype.Timestamptz{Valid: false}
	disabled, err := skdRepo.SaveScheduledTask(appCtx, skd_db.SaveScheduledTaskParams(saved))

	assert.NoError(t, err)
	assert.False(t, disabled.RunAt.Valid)
}
