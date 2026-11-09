package sched_service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/events"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestScheduledBatchActionDefersUntilManualRunCompletes(t *testing.T) {
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool)
	params := overdueTask()
	params.EventName = events.EventNameInvokeBatchAction
	params.Schedule = "FREQ=DAILY"
	params.Owner = "ISIL:MANUAL"
	params.Title = pgtype.Text{String: params.ID, Valid: true}
	params.RunAt = pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}
	params.ActionData = events.EventData{
		CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{
			TaskId: params.ID, Owner: params.Owner, ActionName: string(schedoapi.RequestAging), Selector: "state==CLOSED",
		}},
		CustomData: map[string]any{"interval": "24h"},
	}
	task, err := schedRepo.SaveScheduledTask(appCtx, params)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, schedRepo.DeleteBatchActionEvents(appCtx, task.ID))
		require.NoError(t, schedRepo.DeleteScheduledTask(appCtx, task.ID, nil))
	})
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	request := func() *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		handler.PostBatchActionsIdRun(rr, httptest.NewRequest(http.MethodPost, "/batch_actions/"+task.ID+"/run", nil), task.ID, schedoapi.PostBatchActionsIdRunParams{})
		return rr
	}
	rr := request()
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	manualEvents, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, manualEvents, 1)
	require.Equal(t, events.EventStatusNew, manualEvents[0].EventStatus)

	// Make the saved schedule due after accepting the manual run. Two scheduler
	// replicas must both defer this action while continuing to dispatch other work.
	task.RunAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Minute), Valid: true}
	_, err = schedRepo.SaveScheduledTask(appCtx, sched_db.SaveScheduledTaskParams(task))
	require.NoError(t, err)
	other, err := schedRepo.SaveScheduledTask(appCtx, overdueTask())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, schedRepo.DeleteScheduledTask(appCtx, other.ID, nil)) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := &countingEventBus{}
	startScheduler(t, ctx, bus)
	startScheduler(t, ctx, bus)
	require.Eventually(t, func() bool {
		deferred, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
		return err == nil && deferred.Status == sched_db.ScheduledTaskStatusPending &&
			deferred.RunAt.Valid && deferred.RunAt.Time.After(time.Now()) && bus.totalClaims() > 0
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{other.ID}, bus.getClaims())
	list, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, list, 1, "the due schedule must not overlap the manual run")
	deferred, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
	require.NoError(t, err)
	require.Equal(t, task.Schedule, deferred.Schedule)

	// Complete the manual consumer and make the deferred occurrence due again.
	require.NoError(t, eventRepo.WithTxFunc(appCtx, func(repo events.EventRepo) error {
		if _, err := repo.ClaimEventForSignal(appCtx, manualEvents[0].ID, events.SignalTaskCreated); err != nil {
			return err
		}
		_, err := repo.UpdateEventLifecycle(appCtx, events.UpdateEventLifecycleParams{
			ID: manualEvents[0].ID, EventStatus: events.EventStatusSuccess,
		})
		return err
	}))
	deferred.RunAt = pgtype.Timestamptz{Time: time.Now().Add(-time.Second), Valid: true}
	_, err = schedRepo.SaveScheduledTask(appCtx, sched_db.SaveScheduledTaskParams(deferred))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		rescheduled, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
		return err == nil && rescheduled.Status == sched_db.ScheduledTaskStatusPending &&
			rescheduled.RunAt.Valid && rescheduled.RunAt.Time.After(time.Now())
	}, 5*time.Second, 10*time.Millisecond)
	list, err = eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, list, 2, "the deferred run must be queued exactly once across replicas")
	active := 0
	for _, event := range list {
		if event.EventStatus == events.EventStatusNew {
			active++
		}
	}
	require.Equal(t, 1, active)
	rr = request()
	require.Equal(t, http.StatusConflict, rr.Code, rr.Body.String())
}
