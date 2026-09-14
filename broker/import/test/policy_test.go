package test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/events"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportTemplatePolicies(t *testing.T) {
	owner := uuid.NewString()
	original := pr_db.SaveTemplateParams{ID: uuid.NewString(), Owner: owner, Title: "Original", Purpose: "email", Body: "body", ContentType: "text/plain", Labels: []string{"notice"}, Audience: pgtype.Text{String: "patron", Valid: true}, CreatedAt: testTimestamp(0), UpdatedAt: testTimestamp(1)}
	result, err := importTestRepo.ImportTemplate(importTestCtx, original, importdb.ConflictPolicyFail)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, result.Outcome)

	incoming := original
	incoming.ID = uuid.NewString()
	incoming.Title = "Updated"
	_, err = importTestRepo.ImportTemplate(importTestCtx, incoming, importdb.ConflictPolicyFail)
	var conflict *importdb.ConflictError
	assert.ErrorAs(t, err, &conflict)
	skipped, err := importTestRepo.ImportTemplate(importTestCtx, incoming, importdb.ConflictPolicySkip)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeSkipped, skipped.Outcome)
	updated, err := importTestRepo.ImportTemplate(importTestCtx, incoming, importdb.ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, updated.Outcome)

	var id, title string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT id,title FROM template WHERE owner=$1", owner).Scan(&id, &title))
	assert.Equal(t, original.ID, id)
	assert.Equal(t, "Updated", title)
}

func TestImportTemplateUpdateRejectsAmbiguousLabelOverlap(t *testing.T) {
	owner := uuid.NewString()
	for _, label := range []string{"first", "second"} {
		params := pr_db.SaveTemplateParams{ID: uuid.NewString(), Owner: owner, Title: label, Purpose: "email", Body: "body", ContentType: "text/plain", Labels: []string{label}, Audience: pgtype.Text{String: "patron", Valid: true}, CreatedAt: testTimestamp(0), UpdatedAt: testTimestamp(1)}
		_, err := importTestRepo.ImportTemplate(importTestCtx, params, importdb.ConflictPolicyFail)
		require.NoError(t, err)
	}
	incoming := pr_db.SaveTemplateParams{ID: uuid.NewString(), Owner: owner, Title: "ambiguous", Purpose: "email", Body: "body", ContentType: "text/plain", Labels: []string{"first", "second"}, Audience: pgtype.Text{String: "patron", Valid: true}, CreatedAt: testTimestamp(0), UpdatedAt: testTimestamp(1)}
	_, err := importTestRepo.ImportTemplate(importTestCtx, incoming, importdb.ConflictPolicyUpdate)
	require.ErrorContains(t, err, "labels overlap multiple templates")
	assert.Equal(t, 2, queryCount(t, "SELECT count(*) FROM template WHERE owner=$1", owner))
}

func TestImportBatchActionPolicies(t *testing.T) {
	_, err := importTestPool.Exec(context.Background(), "INSERT INTO event_config(event_name,event_type) VALUES ($1,'scheduled') ON CONFLICT DO NOTHING", events.EventNameInvokeBatchAction)
	require.NoError(t, err)
	owner := uuid.NewString()
	listener, err := importTestPool.Acquire(context.Background())
	require.NoError(t, err)
	defer listener.Release()
	_, err = listener.Exec(context.Background(), "LISTEN "+sched_db.SchedulerChannel)
	require.NoError(t, err)
	originalID := uuid.NewString()
	original := sched_db.SaveScheduledTaskParams{ID: originalID, EventName: events.EventNameInvokeBatchAction, Schedule: "FREQ=DAILY", ActionData: events.EventData{CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{ActionName: "request-aging", Selector: "state = NEW", TaskId: originalID, Owner: owner}}}, Title: pgtype.Text{String: "Daily", Valid: true}, Status: sched_db.ScheduledTaskStatusPending, Owner: owner, CreatedAt: pgtype.Timestamptz{Time: testTimestamp(0).Time, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: testTimestamp(1).Time, Valid: true}}
	result, err := importTestRepo.ImportBatchAction(importTestCtx, original, importdb.ConflictPolicyFail)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, result.Outcome)
	notifyCtx, cancelNotify := context.WithTimeout(context.Background(), time.Second)
	_, err = listener.Conn().WaitForNotification(notifyCtx)
	cancelNotify()
	require.NoError(t, err)

	incoming := original
	incoming.ID = uuid.NewString()
	incoming.ActionData.BatchActionData = &events.BatchActionData{ActionName: "request-aging", Selector: "state = NEW", TaskId: incoming.ID, Owner: owner}
	incoming.Schedule = "FREQ=WEEKLY"
	_, err = importTestRepo.ImportBatchAction(importTestCtx, incoming, importdb.ConflictPolicyFail)
	assert.Error(t, err)
	skipped, err := importTestRepo.ImportBatchAction(importTestCtx, incoming, importdb.ConflictPolicySkip)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeSkipped, skipped.Outcome)
	quietCtx, cancelQuiet := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err = listener.Conn().WaitForNotification(quietCtx)
	cancelQuiet()
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	updated, err := importTestRepo.ImportBatchAction(importTestCtx, incoming, importdb.ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, updated.Outcome)

	var id, schedule string
	var actionData events.EventData
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT id,schedule,action_data FROM scheduled_task WHERE owner=$1", owner).Scan(&id, &schedule, &actionData))
	assert.Equal(t, original.ID, id)
	assert.Equal(t, "FREQ=WEEKLY", schedule)
	require.NotNil(t, actionData.BatchActionData)
	assert.Equal(t, original.ID, actionData.BatchActionData.TaskId)
}

func TestImportBatchActionUpdateIgnoresNonBatchTaskWithSameTitle(t *testing.T) {
	ctx := context.Background()
	_, err := importTestPool.Exec(ctx, `
		INSERT INTO event_config(event_name, event_type)
		VALUES ($1, 'scheduled'), ($2, 'scheduled')
		ON CONFLICT DO NOTHING`, events.EventNameInvokeBatchAction, events.EventNameInvokeBackgroundAction)
	require.NoError(t, err)

	owner := uuid.NewString()
	title := "Shared title"
	backgroundID := uuid.NewString()
	_, err = importTestPool.Exec(ctx, `
		INSERT INTO scheduled_task (id, event_name, schedule, title, status, owner)
		VALUES ($1, $2, 'FREQ=DAILY', $3, 'pending', $4)`,
		backgroundID, events.EventNameInvokeBackgroundAction, title, owner)
	require.NoError(t, err)

	incomingID := uuid.NewString()
	incoming := sched_db.SaveScheduledTaskParams{
		ID:        incomingID,
		EventName: events.EventNameInvokeBatchAction,
		Schedule:  "FREQ=WEEKLY",
		ActionData: events.EventData{CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{
			ActionName: "request-aging",
			Selector:   "state = NEW",
			TaskId:     incomingID,
			Owner:      owner,
		}}},
		Title:     pgtype.Text{String: title, Valid: true},
		Status:    sched_db.ScheduledTaskStatusPending,
		Owner:     owner,
		CreatedAt: pgtype.Timestamptz{Time: testTimestamp(0).Time, Valid: true},
		UpdatedAt: pgtype.Timestamptz{Time: testTimestamp(1).Time, Valid: true},
	}

	result, err := importTestRepo.ImportBatchAction(importTestCtx, incoming, importdb.ConflictPolicyUpdate)
	require.NoError(t, err)
	require.Equal(t, importdb.OutcomeImported, result.Outcome)

	var backgroundCount, importedCount int
	require.NoError(t, importTestPool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE id = $1 AND event_name = $2),
			COUNT(*) FILTER (WHERE id = $3 AND event_name = $4)
		FROM scheduled_task
		WHERE owner = $5 AND title = $6`,
		backgroundID, events.EventNameInvokeBackgroundAction,
		incomingID, events.EventNameInvokeBatchAction,
		owner, title).Scan(&backgroundCount, &importedCount))
	require.Equal(t, 1, backgroundCount)
	require.Equal(t, 1, importedCount)
}
