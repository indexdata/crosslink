package importdb

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportTemplatePolicies(t *testing.T) {
	owner := uuid.NewString()
	original := pr_db.SaveTemplateParams{ID: uuid.NewString(), Owner: owner, Title: "Original", Purpose: "email", Body: "body", ContentType: "text/plain", Labels: []string{"notice"}, Audience: pgtype.Text{String: "patron", Valid: true}, CreatedAt: testTimestamp(0), UpdatedAt: testTimestamp(1)}
	result, err := importTestRepo.ImportTemplate(importTestCtx, original, ConflictPolicyFail)
	require.NoError(t, err)
	assert.Equal(t, OutcomeImported, result.Outcome)

	incoming := original
	incoming.ID = uuid.NewString()
	incoming.Title = "Updated"
	_, err = importTestRepo.ImportTemplate(importTestCtx, incoming, ConflictPolicyFail)
	var conflict *ConflictError
	assert.ErrorAs(t, err, &conflict)
	skipped, err := importTestRepo.ImportTemplate(importTestCtx, incoming, ConflictPolicySkip)
	require.NoError(t, err)
	assert.Equal(t, OutcomeSkipped, skipped.Outcome)
	updated, err := importTestRepo.ImportTemplate(importTestCtx, incoming, ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, OutcomeImported, updated.Outcome)

	var id, title string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT id,title FROM template WHERE owner=$1", owner).Scan(&id, &title))
	assert.Equal(t, original.ID, id)
	assert.Equal(t, "Updated", title)
}

func TestImportTemplateUpdateRejectsAmbiguousLabelOverlap(t *testing.T) {
	owner := uuid.NewString()
	for _, label := range []string{"first", "second"} {
		params := pr_db.SaveTemplateParams{ID: uuid.NewString(), Owner: owner, Title: label, Purpose: "email", Body: "body", ContentType: "text/plain", Labels: []string{label}, Audience: pgtype.Text{String: "patron", Valid: true}, CreatedAt: testTimestamp(0), UpdatedAt: testTimestamp(1)}
		_, err := importTestRepo.ImportTemplate(importTestCtx, params, ConflictPolicyFail)
		require.NoError(t, err)
	}
	incoming := pr_db.SaveTemplateParams{ID: uuid.NewString(), Owner: owner, Title: "ambiguous", Purpose: "email", Body: "body", ContentType: "text/plain", Labels: []string{"first", "second"}, Audience: pgtype.Text{String: "patron", Valid: true}, CreatedAt: testTimestamp(0), UpdatedAt: testTimestamp(1)}
	_, err := importTestRepo.ImportTemplate(importTestCtx, incoming, ConflictPolicyUpdate)
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
	original := sched_db.SaveScheduledTaskParams{ID: uuid.NewString(), EventName: events.EventNameInvokeBatchAction, Schedule: "FREQ=DAILY", ActionData: events.EventData{}, Title: pgtype.Text{String: "Daily", Valid: true}, Status: sched_db.ScheduledTaskStatusPending, Owner: owner, CreatedAt: pgtype.Timestamptz{Time: testTimestamp(0).Time, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: testTimestamp(1).Time, Valid: true}}
	result, err := importTestRepo.ImportBatchAction(importTestCtx, original, ConflictPolicyFail)
	require.NoError(t, err)
	assert.Equal(t, OutcomeImported, result.Outcome)
	notifyCtx, cancelNotify := context.WithTimeout(context.Background(), time.Second)
	_, err = listener.Conn().WaitForNotification(notifyCtx)
	cancelNotify()
	require.NoError(t, err)

	incoming := original
	incoming.ID = uuid.NewString()
	incoming.Schedule = "FREQ=WEEKLY"
	_, err = importTestRepo.ImportBatchAction(importTestCtx, incoming, ConflictPolicyFail)
	assert.Error(t, err)
	skipped, err := importTestRepo.ImportBatchAction(importTestCtx, incoming, ConflictPolicySkip)
	require.NoError(t, err)
	assert.Equal(t, OutcomeSkipped, skipped.Outcome)
	quietCtx, cancelQuiet := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err = listener.Conn().WaitForNotification(quietCtx)
	cancelQuiet()
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	updated, err := importTestRepo.ImportBatchAction(importTestCtx, incoming, ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, OutcomeImported, updated.Outcome)

	var id, schedule string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT id,schedule FROM scheduled_task WHERE owner=$1", owner).Scan(&id, &schedule))
	assert.Equal(t, original.ID, id)
	assert.Equal(t, "FREQ=WEEKLY", schedule)
}
