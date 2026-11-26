package events

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchMigrationClearsDetachedLegacyLocalMessageTasks(t *testing.T) {
	up, err := os.ReadFile("../../migrations/083_batch_worker_leases.up.sql")
	require.NoError(t, err)
	down, err := os.ReadFile("../../migrations/083_batch_worker_leases.down.sql")
	require.NoError(t, err)

	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requestID, standaloneRequestID := uuid.NewString(), uuid.NewString()
	legacyTime := time.Now().UTC().Add(-time.Minute)
	fixtures := []struct {
		id, parent, requestID string
		name                  events.EventName
		status                events.EventStatus
		signal                events.Signal
		interrupted           bool
	}{
		{id: "message", requestID: requestID, name: events.EventNamePatronRequestMessage, status: events.EventStatusProcessing, signal: events.SignalTaskBegin, interrupted: true},
		{id: "inline", parent: "message", requestID: requestID, name: events.EventNameInvokeAction, status: events.EventStatusProcessing, signal: events.SignalTaskBegin, interrupted: true},
		{id: "nested", parent: "inline", requestID: requestID, name: events.EventNameInvokeAction, status: events.EventStatusNew, signal: events.SignalTaskCreated, interrupted: true},
		{id: "completed-message", requestID: requestID, name: events.EventNamePatronRequestMessage, status: events.EventStatusSuccess},
		{id: "unfinished-child-of-completed-message", parent: "completed-message", requestID: requestID, name: events.EventNameInvokeAction, status: events.EventStatusNew, signal: events.SignalTaskCreated, interrupted: true},
		{id: "standalone-action", requestID: standaloneRequestID, name: events.EventNameInvokeAction, status: events.EventStatusNew, signal: events.SignalTaskCreated},
		{id: "standalone-inline", parent: "standalone-action", requestID: standaloneRequestID, name: events.EventNameInvokeAction, status: events.EventStatusNew, signal: events.SignalTaskCreated},
	}
	nextAction := events.Event{
		ID: requestID + "-next", IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID, PatronRequestID: requestID,
		EventType: events.EventTypeTask, EventName: events.EventNameInvokeAction,
		Timestamp: pgtype.Timestamp{Time: legacyTime.Add(time.Second), Valid: true},
	}

	rollback := errors.New("roll back migration fixture")
	err = eventRepo.WithTxFunc(ctx, func(bound events.EventRepo) error {
		db := bound.(*events.PgEventRepo).GetConnOrTx()
		if _, err := db.Exec(ctx, string(down)); err != nil {
			return fmt.Errorf("roll back migration 083: %w", err)
		}
		if _, err := db.Exec(ctx, `INSERT INTO patron_request (id, state, side)
			VALUES ($1, 'NEW', 'borrowing'), ($2, 'NEW', 'borrowing')`, requestID, standaloneRequestID); err != nil {
			return fmt.Errorf("save fixture requests: %w", err)
		}
		for _, fixture := range fixtures {
			_, err := bound.SaveEvent(ctx, events.SaveEventParams{
				ID: requestID + "-" + fixture.id, IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID, PatronRequestID: fixture.requestID,
				EventName: fixture.name, EventType: events.EventTypeTask, EventStatus: fixture.status, LastSignal: string(fixture.signal),
				ParentID:  pgtype.Text{String: requestID + "-" + fixture.parent, Valid: fixture.parent != ""},
				Timestamp: pgtype.Timestamp{Time: legacyTime, Valid: true},
			})
			if err != nil {
				return fmt.Errorf("save fixture %s: %w", fixture.id, err)
			}
		}
		if _, err := bound.GetOlderIncompleteEvent(ctx, nextAction); err != nil {
			return fmt.Errorf("expected old inline actions to block the next action before migrating: %w", err)
		}
		if _, err := db.Exec(ctx, string(up)); err != nil {
			return fmt.Errorf("apply migration 083: %w", err)
		}
		for _, fixture := range fixtures {
			event, err := bound.GetEvent(ctx, requestID+"-"+fixture.id)
			if err != nil {
				return fmt.Errorf("read fixture %s: %w", fixture.id, err)
			}
			if fixture.interrupted {
				assert.Equal(t, events.EventStatusError, event.EventStatus, fixture.id)
				assert.Empty(t, event.LastSignal, fixture.id)
				assert.NotNil(t, event.ResultData.EventError, fixture.id)
			} else {
				assert.Equal(t, fixture.status, event.EventStatus, fixture.id)
				assert.Equal(t, string(fixture.signal), event.LastSignal, fixture.id)
			}
		}
		_, err := bound.GetOlderIncompleteEvent(ctx, nextAction)
		assert.ErrorIs(t, err, pgx.ErrNoRows, "the old local-message actions no longer block exclusive actions")
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}

func TestBatchRollbackClearsBatchAndConfirmationTasks(t *testing.T) {
	down, err := os.ReadFile("../../migrations/083_batch_worker_leases.down.sql")
	require.NoError(t, err)
	ctx := common.CreateExtCtxWithArgs(t.Context(), nil)
	runID := uuid.NewString()
	fixtures := []struct {
		id         string
		name       events.EventName
		status     events.EventStatus
		signal     events.Signal
		batchOwned bool
	}{
		{runID, events.EventNameInvokeBatchAction, events.EventStatusNew, events.SignalTaskCreated, true},
		{uuid.NewString(), events.EventNameInvokeBackgroundAction, events.EventStatusProcessing, events.SignalTaskBegin, true},
		{uuid.NewString(), events.EventNameMessageSupplier, events.EventStatusNew, events.SignalTaskCreated, true},
		{uuid.NewString(), events.EventNameInvokeAction, events.EventStatusSuccess, "", true},
		{uuid.NewString(), events.EventNameConfirmRequesterMsg, events.EventStatusNew, events.SignalTaskCreated, false},
		{uuid.NewString(), events.EventNameConfirmSupplierMsg, events.EventStatusProcessing, events.SignalTaskBegin, false},
		{uuid.NewString(), events.EventNameConfirmSupplierMsg, events.EventStatusSuccess, "", false},
	}
	rollback := errors.New("roll back migration fixture")
	err = eventRepo.WithTxFunc(ctx, func(bound events.EventRepo) error {
		db := bound.(*events.PgEventRepo).GetConnOrTx()
		queries := events.Queries{}
		for _, fixture := range fixtures {
			_, err := bound.SaveEvent(ctx, events.SaveEventParams{
				ID: fixture.id, IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID, PatronRequestID: events.DEFAULT_PATRON_REQUEST_ID,
				EventName: fixture.name, EventType: events.EventTypeTask, EventStatus: fixture.status, LastSignal: string(fixture.signal),
				Timestamp: pgtype.Timestamp{Time: time.Now().UTC(), Valid: true},
			})
			if err != nil {
				return fmt.Errorf("save fixture %s: %w", fixture.id, err)
			}
		}
		if err := queries.AddBatchActionRun(ctx, db, events.AddBatchActionRunParams{ID: runID, ScheduledTaskID: "task", ActionName: "overdue"}); err != nil {
			return fmt.Errorf("save batch run: %w", err)
		}
		for _, fixture := range fixtures {
			if !fixture.batchOwned {
				continue
			}
			err := queries.AddBatchActionRunEvent(ctx, db, events.AddBatchActionRunEventParams{EventID: fixture.id, RunID: runID, WorkerEventID: runID})
			if err != nil {
				return fmt.Errorf("link fixture %s: %w", fixture.id, err)
			}
		}
		if _, err := db.Exec(ctx, string(down)); err != nil {
			return fmt.Errorf("roll back migration 083: %w", err)
		}
		for _, fixture := range fixtures {
			event, err := bound.GetEvent(ctx, fixture.id)
			if err != nil {
				return fmt.Errorf("read fixture %s: %w", fixture.id, err)
			}
			if fixture.status == events.EventStatusSuccess {
				assert.Equal(t, events.EventStatusSuccess, event.EventStatus)
				assert.Nil(t, event.ResultData.EventError)
			} else {
				assert.Equal(t, events.EventStatusError, event.EventStatus, fixture.name)
				assert.Empty(t, event.LastSignal, fixture.name)
				assert.NotNil(t, event.ResultData.EventError, fixture.name)
			}
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
}
