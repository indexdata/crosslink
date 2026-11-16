package sched_service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/events"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestBatchRunsProceedWithUnfinishedEvents(t *testing.T) {
	for _, action := range []string{string(schedoapi.RequestAging), string(schedoapi.EmailPullslips)} {
		t.Run(action, func(t *testing.T) {
			testBatchRunsProceedWithUnfinishedEvents(t, action)
		})
	}
}

func testBatchRunsProceedWithUnfinishedEvents(t *testing.T, action string) {
	t.Helper()
	for _, name := range []events.EventName{events.EventNameInvokeBatchAction, events.EventNameInvokeBackgroundAction} {
		for _, status := range []events.EventStatus{events.EventStatusNew, events.EventStatusProcessing} {
			t.Run(string(name)+"/"+string(status), func(t *testing.T) {
				pool := schedRepo.(*sched_db.PgSchedRepo).Pool
				eventRepo := app.CreateEventRepo(pool)
				params := overdueTask()
				params.EventName = events.EventNameInvokeBatchAction
				params.Schedule = "FREQ=DAILY"
				params.Owner = "ISIL:MANUAL"
				params.Title = pgtype.Text{String: params.ID, Valid: true}
				params.Status = sched_db.ScheduledTaskStatusStopped
				params.RunAt = pgtype.Timestamptz{}
				params.ActionData = events.EventData{
					CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{
						TaskId: params.ID, Owner: params.Owner, ActionName: action, Selector: "state==CLOSED",
					}},
					CustomData: map[string]any{"interval": "24h"},
				}
				if action == string(schedoapi.EmailPullslips) {
					params.ActionData.BatchActionData.Selector = "side==lending and state==WILL_SUPPLY"
					params.ActionData.CustomData = map[string]any{"to": []string{"staff@example.com"}, "templateLabel": "pullslip-email", "includePdf": true}
				}
				task, err := schedRepo.SaveScheduledTask(appCtx, params)
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, schedRepo.DeleteBatchActionEvents(appCtx, task.ID))
					require.NoError(t, schedRepo.DeleteScheduledTask(appCtx, task.ID, nil))
				})

				// Persist an old batch event, or a completed batch with an unfinished
				// child, as can remain after a worker is interrupted.
				parent := events.SaveEventParams{
					ID: uuid.NewString(), Timestamp: pgtype.Timestamp{Time: time.Now().Add(-2 * time.Hour), Valid: true},
					IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID, PatronRequestID: events.DEFAULT_PATRON_REQUEST_ID,
					EventType: events.EventTypeTask, EventName: events.EventNameInvokeBatchAction,
					EventStatus: status, EventData: task.ActionData,
				}
				if name == events.EventNameInvokeBackgroundAction {
					parent.EventStatus = events.EventStatusSuccess
				}
				_, err = eventRepo.SaveEvent(appCtx, parent)
				require.NoError(t, err)
				existingCount := 1
				if name == events.EventNameInvokeBackgroundAction {
					child := parent
					child.ID = uuid.NewString()
					child.ParentID = pgtype.Text{String: parent.ID, Valid: true}
					child.EventName = name
					child.EventStatus = status
					_, err = eventRepo.SaveEvent(appCtx, child)
					require.NoError(t, err)
					existingCount++
				}

				handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
				rr := httptest.NewRecorder()
				handler.PostBatchActionsIdRun(rr, httptest.NewRequest(http.MethodPost, "/batch_actions/"+task.ID+"/run", nil), task.ID, schedoapi.PostBatchActionsIdRunParams{})
				require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
				saved, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
				require.NoError(t, err)
				require.Equal(t, task, saved, "manual runs must preserve the disabled schedule")
				list, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
				require.NoError(t, err)
				require.Len(t, list, existingCount+1)

				// Two scheduler replicas must queue one due occurrence despite the
				// unfinished events, and continue dispatching other due work.
				task.Status = sched_db.ScheduledTaskStatusPending
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
					rescheduled, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
					return err == nil && rescheduled.Status == sched_db.ScheduledTaskStatusPending &&
						rescheduled.RunAt.Valid && rescheduled.RunAt.Time.After(time.Now()) && bus.totalClaims() > 0
				}, 5*time.Second, 10*time.Millisecond)
				require.Equal(t, []string{other.ID}, bus.getClaims())
				list, err = eventRepo.GetBatchActionEvents(appCtx, task.ID)
				require.NoError(t, err)
				require.Len(t, list, existingCount+2, "the due occurrence must be queued once across replicas")
				saved, err = schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
				require.NoError(t, err)
				require.Equal(t, task.Schedule, saved.Schedule)
			})
		}
	}
}

func saveEmailBatchRunTask(t *testing.T) sched_db.ScheduledTask {
	t.Helper()
	params := overdueTask()
	params.EventName = events.EventNameInvokeBatchAction
	params.Schedule = "FREQ=DAILY"
	params.Owner = "ISIL:EMAIL"
	params.Title = pgtype.Text{String: params.ID, Valid: true}
	params.Status = sched_db.ScheduledTaskStatusStopped
	params.RunAt = pgtype.Timestamptz{}
	params.ActionData = events.EventData{
		CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{
			TaskId: params.ID, Owner: params.Owner, ActionName: string(schedoapi.EmailPullslips), Selector: "side==lending and state==WILL_SUPPLY",
		}},
		CustomData: map[string]any{"to": []string{"staff@example.com"}, "templateLabel": "pullslip-email", "includePdf": true},
	}
	task, err := schedRepo.SaveScheduledTask(appCtx, params)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, schedRepo.DeleteBatchActionEvents(appCtx, task.ID))
		require.NoError(t, schedRepo.DeleteScheduledTask(appCtx, task.ID, nil))
	})
	return task
}

func TestConcurrentManualEmailRunsEachQueue(t *testing.T) {
	task := saveEmailBatchRunTask(t)
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		go func() {
			<-start
			rr := httptest.NewRecorder()
			handler.PostBatchActionsIdRun(rr, httptest.NewRequest(http.MethodPost, "/batch_actions/"+task.ID+"/run", nil), task.ID, schedoapi.PostBatchActionsIdRunParams{})
			responses <- rr
		}()
	}
	close(start)
	var statuses []int
	for range 2 {
		select {
		case rr := <-responses:
			statuses = append(statuses, rr.Code)
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent manual email run did not return")
		}
	}
	require.ElementsMatch(t, []int{http.StatusAccepted, http.StatusAccepted}, statuses)
	list, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	saved, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
	require.NoError(t, err)
	require.Equal(t, task, saved, "manual runs must preserve the disabled schedule")
}
