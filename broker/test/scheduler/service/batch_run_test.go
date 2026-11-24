package sched_service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/repo"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func saveBatchRunTask(t *testing.T, owner string, action schedoapi.BatchActionName, due bool) sched_db.ScheduledTask {
	t.Helper()
	params := overdueTask()
	params.EventName = events.EventNameInvokeBatchAction
	params.Schedule = "FREQ=DAILY"
	params.Owner = owner
	params.Title = pgtype.Text{String: params.ID, Valid: true}
	if !due {
		params.Status = sched_db.ScheduledTaskStatusStopped
		params.RunAt = pgtype.Timestamptz{}
	}
	params.ActionData = events.EventData{
		CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{
			TaskId: params.ID, Owner: owner, ActionName: string(action), Selector: "state==CLOSED",
		}},
		CustomData: map[string]any{"interval": "24h"},
	}
	if action == schedoapi.EmailPullslips {
		params.ActionData.BatchActionData.Selector = "side==lending and state==WILL_SUPPLY"
		params.ActionData.CustomData = map[string]any{"to": []string{"staff@example.com"}, "templateLabel": "pullslip-email", "includePdf": true}
	}
	task, err := schedRepo.SaveScheduledTask(appCtx, params)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, schedRepo.DeleteBatchActionEvents(appCtx, task.ID))
		require.NoError(t, schedRepo.DeleteScheduledTask(appCtx, task.ID, nil))
	})
	return task
}

func requestBatchRun(handler schedapi.SchedulerApiHandler, taskID string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	handler.PostBatchActionsIdRun(rr, httptest.NewRequest(http.MethodPost, "/batch_actions/"+taskID+"/run", nil), taskID, schedoapi.PostBatchActionsIdRunParams{})
	return rr
}

func TestConcurrentManualBatchRunsReserveOwnerAndAction(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action schedoapi.BatchActionName
		owners [2]string
	}{
		{"same owner email schedules", schedoapi.EmailPullslips, [2]string{"ISIL:MANUAL", "ISIL:MANUAL"}},
		{"same owner request aging", schedoapi.RequestAging, [2]string{"ISIL:MANUAL", "ISIL:MANUAL"}},
		{"unrestricted and scoped", schedoapi.RequestAging, [2]string{"", "ISIL:MANUAL"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks := []sched_db.ScheduledTask{
				saveBatchRunTask(t, tc.owners[0], tc.action, false),
				saveBatchRunTask(t, tc.owners[1], tc.action, false),
			}
			eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
			handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
			start := make(chan struct{})
			responses := make(chan int, len(tasks))
			for _, task := range tasks {
				go func() {
					<-start
					responses <- requestBatchRun(handler, task.ID).Code
				}()
			}
			close(start)
			var statuses []int
			var totalRuns int
			for _, task := range tasks {
				select {
				case status := <-responses:
					statuses = append(statuses, status)
				case <-time.After(5 * time.Second):
					t.Fatal("concurrent manual run did not return")
				}
				saved, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
				require.NoError(t, err)
				require.Equal(t, task, saved, "manual runs must preserve the disabled schedule")
			}
			for _, task := range tasks {
				list, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
				require.NoError(t, err)
				totalRuns += len(list)
			}
			require.ElementsMatch(t, []int{http.StatusAccepted, http.StatusConflict}, statuses)
			require.Equal(t, 1, totalRuns)
		})
	}
}

func TestScheduledAndManualBatchRunsShareReservation(t *testing.T) {
	manual := saveBatchRunTask(t, "ISIL:SHARED", schedoapi.EmailPullslips, false)
	scheduled := saveBatchRunTask(t, manual.Owner, schedoapi.EmailPullslips, true)
	other := saveBatchRunTask(t, manual.Owner, schedoapi.RequestAging, true)
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := &countingEventBus{}
	startScheduler(t, ctx, bus)
	startScheduler(t, ctx, bus)
	response := requestBatchRun(handler, manual.ID)
	require.Contains(t, []int{http.StatusAccepted, http.StatusConflict}, response.Code, response.Body.String())

	for _, task := range []sched_db.ScheduledTask{scheduled, other} {
		require.Eventually(t, func() bool {
			saved, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
			return err == nil && saved.Status == sched_db.ScheduledTaskStatusPending &&
				saved.RunAt.Valid && saved.RunAt.Time.After(time.Now())
		}, 5*time.Second, 10*time.Millisecond)
	}
	manualRuns, err := eventRepo.GetBatchActionEvents(appCtx, manual.ID)
	require.NoError(t, err)
	scheduledRuns, err := eventRepo.GetBatchActionEvents(appCtx, scheduled.ID)
	require.NoError(t, err)
	otherRuns, err := eventRepo.GetBatchActionEvents(appCtx, other.ID)
	require.NoError(t, err)
	require.Len(t, otherRuns, 1)
	require.Equal(t, 1, len(manualRuns)+len(scheduledRuns))
	if response.Code == http.StatusConflict {
		require.Len(t, scheduledRuns, 1)
	} else {
		require.Len(t, manualRuns, 1)
	}
}

func TestScheduledBatchRunSkipsOverlappingUnrestrictedScope(t *testing.T) {
	for _, tc := range []struct {
		name           string
		activeOwner    string
		scheduledOwner string
	}{
		{"unrestricted run blocks scoped schedule", "", "ISIL:SHARED"},
		{"scoped run blocks unrestricted schedule", "ISIL:SHARED", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			active := saveBatchRunTask(t, tc.activeOwner, schedoapi.RequestAging, false)
			due := saveBatchRunTask(t, tc.scheduledOwner, schedoapi.RequestAging, true)
			differentAction := saveBatchRunTask(t, tc.scheduledOwner, schedoapi.EmailPullslips, true)
			eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
			handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
			require.Equal(t, http.StatusAccepted, requestBatchRun(handler, active.ID).Code)
			require.Equal(t, http.StatusConflict, requestBatchRun(handler, due.ID).Code)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			startScheduler(t, ctx, &countingEventBus{})
			for _, task := range []sched_db.ScheduledTask{due, differentAction} {
				require.Eventually(t, func() bool {
					saved, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
					return err == nil && saved.Status == sched_db.ScheduledTaskStatusPending &&
						saved.RunAt.Valid && saved.RunAt.Time.After(time.Now())
				}, 5*time.Second, 10*time.Millisecond)
			}
			runs, err := eventRepo.GetBatchActionEvents(appCtx, due.ID)
			require.NoError(t, err)
			require.Empty(t, runs)
			runs, err = eventRepo.GetBatchActionEvents(appCtx, differentAction.ID)
			require.NoError(t, err)
			require.Len(t, runs, 1)
		})
	}
}

func TestBatchRunReservationLastsUntilDescendantsComplete(t *testing.T) {
	task := saveBatchRunTask(t, "ISIL:CHILD", schedoapi.RequestAging, false)
	other := saveBatchRunTask(t, task.Owner, schedoapi.RequestAging, false)
	differentOwner := saveBatchRunTask(t, "ISIL:INDEPENDENT", schedoapi.RequestAging, false)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
	list, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	parentID := list[0].ID

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := events.NewPostgresEventBus(eventRepo, connString)
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))

	parentWorker := claimBatchWorker(t, eventRepo, bus, parentID)
	_, err = parentWorker.BeginTask(parentID, events.SignalObservers)
	require.NoError(t, err)
	childID, err := parentWorker.CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeBackgroundAction,
		task.ActionData, events.EventDomainPatronRequest, &parentID, events.SignalObservers)
	require.NoError(t, err)
	childWorker := claimBatchWorker(t, eventRepo, bus, childID)
	_, err = childWorker.BeginTask(childID, events.SignalObservers)
	require.NoError(t, err)
	// Inline auto actions carry a parent ID with no batch metadata.
	grandchildID, err := childWorker.CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeAction,
		events.EventData{}, events.EventDomainPatronRequest, &childID, events.SignalObservers)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(appCtx, "DELETE FROM event WHERE id = $1", grandchildID)
		require.NoError(t, err)
	})

	grandchildWorker := claimBatchWorker(t, eventRepo, bus, grandchildID)
	_, err = parentWorker.CompleteTask(parentID, nil, events.EventStatusSuccess, events.SignalObservers)
	require.NoError(t, err)
	_, err = childWorker.CompleteTask(childID, nil, events.EventStatusSuccess, events.SignalObservers)
	require.NoError(t, err)
	lateID, err := parentWorker.CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeBackgroundAction,
		task.ActionData, events.EventDomainPatronRequest, &parentID, events.SignalObservers)
	require.ErrorIs(t, err, repo.ErrBatchAttemptLost)
	_, err = eventRepo.GetEvent(appCtx, lateID)
	require.Error(t, err, "late child creation must roll back")

	active, err := schedRepo.HasActiveBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.True(t, active)
	require.Equal(t, http.StatusConflict, requestBatchRun(handler, other.ID).Code)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, differentOwner.ID).Code)

	_, err = grandchildWorker.BeginTask(grandchildID, events.SignalObservers)
	require.NoError(t, err)
	_, err = grandchildWorker.CompleteTask(grandchildID, nil, events.EventStatusError, events.SignalObservers)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, other.ID).Code)
}

func TestBatchRunReservationRollsBackWithQueue(t *testing.T) {
	task := saveBatchRunTask(t, "ISIL:ROLLBACK", schedoapi.EmailPullslips, false)
	rollback := errors.New("roll back queue transaction")
	err := schedRepo.WithTxFunc(appCtx, func(tx sched_db.SchedRepo) error {
		locked, err := tx.GetScheduledTaskByIdForUpdate(appCtx, task.ID, nil)
		if err != nil {
			return err
		}
		if err = tx.QueueBatchActionRun(appCtx, locked); err != nil {
			return err
		}
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	list, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Empty(t, list)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
}

func TestBatchRunReleasesReservationWhenLastChildIsDeleted(t *testing.T) {
	t.Run("after sibling completes", func(t *testing.T) { testBatchRunChildDeletion(t, false) })
	t.Run("while sibling completes", func(t *testing.T) { testBatchRunChildDeletion(t, true) })
}

func testBatchRunChildDeletion(t *testing.T, concurrent bool) {
	t.Helper()
	task := saveBatchRunTask(t, "ISIL:DELETED-CHILD", schedoapi.RequestAging, false)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
	runs, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	root := runs[0]
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := events.NewPostgresEventBus(eventRepo, connString)
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	parentWorker := claimBatchWorker(t, eventRepo, bus, root.ID)
	_, err = parentWorker.BeginTask(root.ID, events.SignalObservers)
	require.NoError(t, err)

	prRepo := pr_db.CreatePrRepo(pool, false)
	prID := uuid.NewString()
	_, err = prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: prID, CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		Language: "english", Items: []pr_db.PrItem{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prRepo.DeletePatronRequest(appCtx, prID)) })
	childID, err := parentWorker.CreateTask(prID, events.EventNameInvokeBackgroundAction, task.ActionData,
		events.EventDomainPatronRequest, &root.ID, events.SignalObservers)
	require.NoError(t, err)
	otherChildID, err := parentWorker.CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeBackgroundAction, task.ActionData,
		events.EventDomainPatronRequest, &root.ID, events.SignalObservers)
	require.NoError(t, err)
	otherWorker := claimBatchWorker(t, eventRepo, bus, otherChildID)
	_, err = otherWorker.BeginTask(otherChildID, events.SignalObservers)
	require.NoError(t, err)
	_, err = parentWorker.CompleteTask(root.ID, nil, events.EventStatusSuccess, events.SignalObservers)
	require.NoError(t, err)

	if concurrent {
		results := make(chan error, 2)
		go func() { results <- prRepo.DeletePatronRequest(appCtx, prID) }()
		go func() {
			_, err := otherWorker.CompleteTask(otherChildID, nil, events.EventStatusSuccess, events.SignalObservers)
			results <- err
		}()
		for range 2 {
			select {
			case err := <-results:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("concurrent deletion and completion did not return")
			}
		}
	} else {
		_, err := otherWorker.CompleteTask(otherChildID, nil, events.EventStatusSuccess, events.SignalObservers)
		require.NoError(t, err)
		require.Equal(t, http.StatusConflict, requestBatchRun(handler, task.ID).Code)
		rollback := errors.New("roll back request deletion")
		err = prRepo.WithTxFunc(appCtx, func(tx pr_db.PrRepo) error {
			if err := tx.DeletePatronRequest(appCtx, prID); err != nil {
				return err
			}
			return rollback
		})
		require.ErrorIs(t, err, rollback)
		require.Equal(t, http.StatusConflict, requestBatchRun(handler, task.ID).Code)
		require.NoError(t, prRepo.DeletePatronRequest(appCtx, prID))
	}
	_, err = eventRepo.GetEvent(appCtx, childID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
}

func TestMalformedBatchScheduleDoesNotBlockOtherDueWork(t *testing.T) {
	bad := overdueTask()
	bad.EventName = events.EventNameInvokeBatchAction
	bad.Schedule = "FREQ=DAILY"
	bad.Title = pgtype.Text{String: bad.ID, Valid: true}
	bad.RunAt.Time = time.Now().Add(-time.Hour)
	_, err := schedRepo.SaveScheduledTask(appCtx, bad)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, schedRepo.DeleteScheduledTask(appCtx, bad.ID, nil)) })
	good := overdueTask()
	_, err = schedRepo.SaveScheduledTask(appCtx, good)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, schedRepo.DeleteScheduledTask(appCtx, good.ID, nil)) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := &countingEventBus{}
	startScheduler(t, ctx, bus)
	require.Eventually(t, func() bool { return bus.totalClaims() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{good.ID}, bus.getClaims())
	stopped, err := schedRepo.GetScheduledTaskById(appCtx, bad.ID, nil)
	require.NoError(t, err)
	require.Equal(t, sched_db.ScheduledTaskStatusStopped, stopped.Status)
	require.False(t, stopped.RunAt.Valid)
	require.Equal(t, bad.Schedule, stopped.Schedule)
}

func claimBatchWorker(t *testing.T, eventRepo events.EventRepo, bus events.EventBus, eventID string) events.EventBus {
	t.Helper()
	_, attempt, err := eventRepo.(*events.PgEventRepo).ClaimEventAttemptForSignal(appCtx, eventID, events.SignalTaskCreated)
	require.NoError(t, err)
	require.NotEmpty(t, attempt.ID)
	return events.WithWorkerContext(bus, repo.WithBatchAttempt(appCtx, attempt))
}

func TestBatchWorkerAttemptFencesWrites(t *testing.T) {
	task := saveBatchRunTask(t, "ISIL:ATTEMPT", schedoapi.RequestAging, false)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
	runs, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	root := runs[0]

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := events.NewPostgresEventBus(eventRepo, connString)
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	_, err = bus.BeginTask(root.ID, events.SignalObservers)
	require.ErrorIs(t, err, repo.ErrBatchAttemptLost)

	type claimResult struct {
		attempt repo.BatchAttempt
		err     error
	}
	results := make(chan claimResult, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			_, attempt, err := eventRepo.ClaimEventAttemptForSignal(appCtx, root.ID, events.SignalTaskCreated)
			results <- claimResult{attempt: attempt, err: err}
		}()
	}
	close(start)
	var owner repo.BatchAttempt
	for range 2 {
		result := <-results
		if result.err != nil {
			require.ErrorIs(t, result.err, pgx.ErrNoRows)
		} else {
			require.Empty(t, owner.ID, "only one consumer may own the attempt")
			owner = result.attempt
		}
	}
	require.NotEmpty(t, owner.ID)
	require.Equal(t, root.ID, owner.EventID)
	claimed, err := eventRepo.GetEvent(appCtx, root.ID)
	require.NoError(t, err)
	require.Empty(t, claimed.LastSignal)

	workCtx := repo.WithBatchAttempt(appCtx, owner)
	worker := events.WithWorkerContext(bus, workCtx)
	_, err = worker.BeginTask(root.ID, events.SignalObservers)
	require.NoError(t, err)

	prRepo := pr_db.CreatePrRepo(pool, false)
	prID := uuid.NewString()
	_, err = prRepo.CreatePatronRequest(workCtx, pr_db.CreatePatronRequestParams{
		ID: prID, CreatedAt: pgtype.Timestamp{Time: time.Now(), Valid: true},
		Language: "english", Items: []pr_db.PrItem{},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prRepo.DeletePatronRequest(appCtx, prID)) })

	// Inline actions claim independently, using the current parent's attempt.
	childID, err := worker.CreateTask(prID, events.EventNameInvokeAction, events.EventData{},
		events.EventDomainPatronRequest, &root.ID, events.SignalObservers)
	require.NoError(t, err)
	_, err = bus.ProcessTask(workCtx, events.Event{ID: childID}, events.SignalObservers,
		func(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
			childAttempt := repo.BatchAttemptFrom(ctx)
			require.Equal(t, event.ID, childAttempt.EventID)
			require.NotEmpty(t, childAttempt.ID)
			require.NotEqual(t, owner.ID, childAttempt.ID)
			return events.EventStatusSuccess, nil
		})
	require.NoError(t, err)

	// Age, cancellation, and even a repeated creation signal leave ownership intact.
	cancelledCtx, cancelWorker := context.WithCancel(appCtx)
	cancelWorker()
	cancelledWorker := events.WithWorkerContext(bus, repo.WithBatchAttempt(common.CreateExtCtxWithArgs(cancelledCtx, nil), owner))
	_, err = cancelledWorker.CompleteTask(root.ID, nil, events.EventStatusSuccess, events.SignalObservers)
	require.ErrorIs(t, err, context.Canceled)
	_, err = pool.Exec(appCtx, "UPDATE batch_action_run_event SET attempt_claimed_at = now() - interval '1 day' WHERE event_id = $1", root.ID)
	require.NoError(t, err)
	_, err = pool.Exec(appCtx, "UPDATE event SET last_signal = $2 WHERE id = $1", root.ID, string(events.SignalTaskCreated))
	require.NoError(t, err)
	_, _, err = eventRepo.ClaimEventAttemptForSignal(appCtx, root.ID, events.SignalTaskCreated)
	require.ErrorIs(t, err, pgx.ErrNoRows)

	// Change the stored token directly to exercise the stale-worker fence.
	// Production code in this step has no operation that replaces an attempt.
	replacement := repo.BatchAttempt{EventID: root.ID, ID: uuid.NewString()}
	_, err = pool.Exec(appCtx, `WITH locked AS (SELECT id FROM event WHERE id = $1 FOR UPDATE)
		UPDATE batch_action_run_event SET attempt_id = $2 FROM locked WHERE event_id = locked.id`, root.ID, replacement.ID)
	require.NoError(t, err)
	_, err = prRepo.CreatePatronRequest(workCtx, pr_db.CreatePatronRequestParams{ID: uuid.NewString()})
	require.ErrorIs(t, err, repo.ErrBatchAttemptLost)
	err = prRepo.WithTxFunc(workCtx, func(pr_db.PrRepo) error {
		t.Error("a stale attempt reached the state transaction")
		return nil
	})
	require.ErrorIs(t, err, repo.ErrBatchAttemptLost)
	rejectedID, err := worker.CreateTask(prID, events.EventNameInvokeBackgroundAction, task.ActionData,
		events.EventDomainPatronRequest, &root.ID, events.SignalObservers)
	require.ErrorIs(t, err, repo.ErrBatchAttemptLost)
	_, err = eventRepo.GetEvent(appCtx, rejectedID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = bus.CreateTask(prID, events.EventNameInvokeBackgroundAction, task.ActionData,
		events.EventDomainPatronRequest, &root.ID, events.SignalObservers)
	require.ErrorIs(t, err, repo.ErrBatchAttemptLost)
	_, err = worker.CompleteTask(root.ID, nil, events.EventStatusSuccess, events.SignalObservers)
	require.ErrorIs(t, err, repo.ErrBatchAttemptLost)
	require.Equal(t, http.StatusConflict, requestBatchRun(handler, task.ID).Code)

	replacementWorker := events.WithWorkerContext(bus, repo.WithBatchAttempt(appCtx, replacement))
	_, err = replacementWorker.CompleteTask(root.ID, nil, events.EventStatusSuccess, events.SignalObservers)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
}
