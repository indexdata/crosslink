package sched_service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/cql-go/pgcql"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/repo"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	schedservice "github.com/indexdata/crosslink/broker/scheduler/service"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type batchPlanRequests struct {
	pr_db.PrRepo
	selected   []pr_db.PatronRequest
	selections int
}

func (r *batchPlanRequests) ListPatronRequests(common.ExtendedContext, pr_db.ListPatronRequestsParams, pgcql.Query) ([]pr_db.PatronRequest, int64, error) {
	r.selections++
	return r.selected, int64(len(r.selected)), nil
}

func TestBatchRequestAgingReusesCommittedPlanAndChild(t *testing.T) {
	task := saveBatchRunTask(t, "ISIL:PLAN", schedoapi.RequestAging, false)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	prRepo := pr_db.CreatePrRepo(pool, false)
	pr, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: uuid.NewString(), CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		Language: "english", Items: []pr_db.PrItem{}, Side: prservice.SideBorrowing, State: prservice.BorrowerStateNew,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prRepo.DeletePatronRequest(appCtx, pr.ID)) })
	eventRepo, bus, root, workCtx := startProgressRun(t, task)
	requests := &batchPlanRequests{selected: []pr_db.PatronRequest{pr}}
	service := schedservice.NewBatchActionService(bus, requests, schedRepo, nil)
	_, err = bus.ProcessTask(workCtx, root, events.SignalObservers,
		func(ctx common.ExtendedContext, root events.Event) (events.EventStatus, *events.EventResult) {
			status, result := service.RequestAging(ctx, root)
			require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
			plan, err := schedRepo.GetBatchActionPlanEntries(ctx, root.ID, 0)
			require.NoError(t, err)
			require.Equal(t, []sched_db.BatchActionPlanEntry{
				{Position: 1, RequestID: pr.ID, Action: string(prservice.BorrowerActionCloseRequest)},
			}, plan)
			children, err := eventRepo.GetPatronRequestEvents(ctx, pr.ID)
			require.NoError(t, err)
			require.Len(t, children, 1)
			staleCtx := repo.WithBatchAttempt(ctx, repo.BatchAttempt{EventID: root.ID, ID: uuid.NewString()})
			_, err = events.WithWorkerContext(bus, staleCtx).CreateTask(pr.ID, children[0].EventName, children[0].EventData,
				events.EventDomainPatronRequest, &root.ID, events.SignalConsumers)
			require.ErrorIs(t, err, repo.ErrBatchAttemptLost)
			listener, err := pgx.Connect(appCtx, connString)
			require.NoError(t, err)
			defer listener.Close(appCtx)
			_, err = listener.Exec(appCtx, "LISTEN "+events.EVENT_BUS_CHANNEL)
			require.NoError(t, err)
			status, result = service.RequestAging(ctx, root)
			require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
			waitCtx, cancel := context.WithTimeout(appCtx, 3*time.Second)
			defer cancel()
			notification, err := listener.WaitForNotification(waitCtx)
			require.NoError(t, err)
			var signal events.NotifyData
			require.NoError(t, json.Unmarshal([]byte(notification.Payload), &signal))
			require.Equal(t, events.NotifyData{Event: children[0].ID, Signal: events.SignalTaskCreated, Target: events.SignalConsumers}, signal)
			child, childAttempt, err := eventRepo.ClaimEventAttemptForSignal(appCtx, children[0].ID, events.SignalTaskCreated)
			require.NoError(t, err)
			childCtx := repo.WithBatchAttempt(appCtx, childAttempt)
			_, err = pool.Exec(appCtx, `
				CREATE FUNCTION test_reject_start_checkpoint() RETURNS trigger LANGUAGE plpgsql AS $$
				BEGIN RAISE EXCEPTION 'checkpoint unavailable'; END;
				$$;
				CREATE TRIGGER test_reject_start_checkpoint BEFORE UPDATE OF work_started_at ON batch_action_run_event
				FOR EACH ROW EXECUTE FUNCTION test_reject_start_checkpoint();
			`)
			require.NoError(t, err)
			_, startErr := bus.ProcessTask(childCtx, child, events.SignalObservers,
				func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
					t.Error("a failed checkpoint must prevent the handler from running")
					return events.EventStatusSuccess, nil
				})
			_, err = pool.Exec(appCtx, `
				DROP TRIGGER test_reject_start_checkpoint ON batch_action_run_event;
				DROP FUNCTION test_reject_start_checkpoint();
			`)
			require.NoError(t, err)
			require.ErrorContains(t, startErr, "checkpoint unavailable")
			unchanged, err := eventRepo.GetEvent(appCtx, child.ID)
			require.NoError(t, err)
			require.Equal(t, events.EventStatusNew, unchanged.EventStatus)
			completed, err := bus.ProcessTask(childCtx, child, events.SignalObservers,
				func(_ common.ExtendedContext, current events.Event) (events.EventStatus, *events.EventResult) {
					var started pgtype.Timestamptz
					require.NoError(t, pool.QueryRow(appCtx, "SELECT work_started_at FROM batch_action_run_event WHERE event_id = $1", current.ID).Scan(&started))
					require.True(t, started.Valid)
					return events.EventStatusSuccess, &events.EventResult{CommonEventData: events.CommonEventData{Note: "already completed"}}
				})
			require.NoError(t, err)
			requests.selected = nil

			status, result = service.RequestAging(ctx, root)
			require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
			require.Equal(t, 1, requests.selections, "a saved plan bypasses reselection")
			children, err = eventRepo.GetPatronRequestEvents(ctx, pr.ID)
			require.NoError(t, err)
			require.Len(t, children, 1)
			require.Equal(t, completed.ID, children[0].ID)
			require.Equal(t, events.EventStatusSuccess, children[0].EventStatus)
			require.Equal(t, "already completed", children[0].ResultData.Note)
			return status, result
		})
	require.NoError(t, err)
}

func startProgressRun(t *testing.T, task sched_db.ScheduledTask) (*events.PgEventRepo, *events.PostgresEventBus, events.Event, common.ExtendedContext) {
	t.Helper()
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
	runs, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bus := events.NewPostgresEventBus(eventRepo, connString)
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	root, attempt, err := eventRepo.ClaimEventAttemptForSignal(appCtx, runs[0].ID, events.SignalTaskCreated)
	require.NoError(t, err)
	workCtx := repo.WithBatchAttempt(appCtx, attempt)
	return eventRepo, bus, root, workCtx
}

type overduePlanRequests struct {
	pr_db.PrRepo
	page func(int32, string, pgcql.Query) ([]pr_db.PatronRequest, error)
}

func (r overduePlanRequests) ListOverdueBatchCandidates(_ common.ExtendedContext, limit int32, after string, query pgcql.Query) ([]pr_db.PatronRequest, error) {
	return r.page(limit, after, query)
}

func TestOverduePlanResumesFromCommittedPageBeforeDispatch(t *testing.T) {
	task := saveBatchRunTask(t, "ISIL:OVERDUE-PLAN", schedoapi.Overdue, false)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	prRepo := pr_db.CreatePrRepo(pool, false)
	createRequest := func(id string) pr_db.PatronRequest {
		pr, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
			ID: id, CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
			Language: "english", Items: []pr_db.PrItem{}, Side: prservice.SideLending, State: prservice.LenderStateReceived,
		})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, prRepo.DeletePatronRequest(appCtx, pr.ID)) })
		return pr
	}
	first := createRequest("a-" + uuid.NewString())
	last := createRequest("z-" + uuid.NewString())
	query, err := pr_db.ParsePatronRequestsCql(fmt.Sprintf("id == %q or id == %q sortBy id/sort.descending", first.ID, last.ID))
	require.NoError(t, err)
	rows, err := prRepo.ListOverdueBatchCandidates(appCtx, 1, "", query)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, first.ID, rows[0].ID)
	rows, err = prRepo.ListOverdueBatchCandidates(appCtx, 1, first.ID, query)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, last.ID, rows[0].ID)
	page := []pr_db.PatronRequest{first}
	for i := 1; i < sched_db.BatchActionPlanPageSize; i++ {
		// The cursor advances past scanned records even if they have no overdue action.
		page = append(page, pr_db.PatronRequest{ID: fmt.Sprintf("m-%03d", i),
			Side: prservice.SideLending, State: prservice.LenderStateShipped})
	}
	afterFirst := page[len(page)-1].ID
	eventRepo, bus, root, workCtx := startProgressRun(t, task)
	pageCalls := 0
	var queryArgs []any
	var cutoff time.Time
	readPage := func(limit int32, after string, query pgcql.Query) ([]pr_db.PatronRequest, error) {
		require.EqualValues(t, sched_db.BatchActionPlanPageSize, limit)
		pageCalls++
		switch pageCalls {
		case 1:
			require.Empty(t, after)
			queryArgs = append([]any(nil), query.GetQueryArguments()...)
			return page, nil
		case 2:
			require.Equal(t, afterFirst, after)
			return nil, errors.New("interrupted after first page")
		case 3:
			require.Equal(t, afterFirst, after)
			require.Equal(t, queryArgs, query.GetQueryArguments())
			children, err := eventRepo.GetPatronRequestEvents(appCtx, first.ID)
			require.NoError(t, err)
			require.Empty(t, children, "dispatch waits for a complete plan")
			return []pr_db.PatronRequest{last}, nil
		default:
			t.Fatalf("a complete plan must not be reselected, read page %d", pageCalls)
			return nil, nil
		}
	}
	service := schedservice.NewBatchActionService(bus, overduePlanRequests{page: readPage}, schedRepo, nil)
	_, err = bus.ProcessTask(workCtx, root, events.SignalObservers,
		func(ctx common.ExtendedContext, root events.Event) (events.EventStatus, *events.EventResult) {
			status, result := service.Overdue(ctx, root)
			require.Equal(t, events.EventStatusError, status, "%+v", result)
			require.Contains(t, result.EventError.Cause, "interrupted after first page")
			progress, err := schedRepo.StartOverdueBatchPlan(ctx, root.ID)
			require.NoError(t, err)
			require.False(t, progress.Complete)
			require.Equal(t, afterFirst, progress.Cursor)
			cutoff = progress.Cutoff
			require.Eventually(t, func() bool { return time.Now().Truncate(time.Second).After(cutoff) }, 2*time.Second, 10*time.Millisecond)
			plan, err := schedRepo.GetBatchActionPlanEntries(ctx, root.ID, 0)
			require.NoError(t, err)
			require.Len(t, plan, 1)
			require.Equal(t, first.ID, plan[0].RequestID)

			status, result = service.Overdue(ctx, root)
			require.Equal(t, events.EventStatusSuccess, status, "%+v", result)
			require.Equal(t, 3, pageCalls)
			progress, err = schedRepo.StartOverdueBatchPlan(ctx, root.ID)
			require.NoError(t, err)
			require.True(t, progress.Complete)
			require.Equal(t, last.ID, progress.Cursor)
			require.Equal(t, cutoff, progress.Cutoff)
			plan, err = schedRepo.GetBatchActionPlanEntries(ctx, root.ID, 0)
			require.NoError(t, err)
			require.Equal(t, []sched_db.BatchActionPlanEntry{
				{Position: 1, RequestID: first.ID, Action: string(prservice.LenderActionOverdue)},
				{Position: 2, RequestID: last.ID, Action: string(prservice.LenderActionOverdue)},
			}, plan)
			for _, requestID := range []string{first.ID, last.ID} {
				children, err := eventRepo.GetPatronRequestEvents(ctx, requestID)
				require.NoError(t, err)
				require.Len(t, children, 1)
			}
			return status, result
		})
	require.NoError(t, err)
}
