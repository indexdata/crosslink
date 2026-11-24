package sched_service

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/repo"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func recoveryRoot(t *testing.T, action schedoapi.BatchActionName) (sched_db.ScheduledTask, events.Event) {
	t.Helper()
	task := saveBatchRunTask(t, "ISIL:"+uuid.NewString(), action, false)
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusAccepted, requestBatchRun(handler, task.ID).Code)
	runs, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	return task, runs[0]
}

func recoveryAttempt(t *testing.T, eventID string, status events.EventStatus, lockID string) repo.BatchAttempt {
	t.Helper()
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)
	_, attempt, err := eventRepo.ClaimEventAttemptForSignal(appCtx, eventID, events.SignalTaskCreated)
	require.NoError(t, err)
	_, err = pool.Exec(appCtx, "UPDATE event SET event_status = $2 WHERE id = $1", eventID, status)
	require.NoError(t, err)
	if lockID != "" {
		_, err = pool.Exec(appCtx, "UPDATE batch_action_run_event SET attempt_lock_id = $2 WHERE event_id = $1", eventID, lockID)
		require.NoError(t, err)
	}
	return attempt
}

func TestBatchRecoveryAtStartupAndPeriodically(t *testing.T) {
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)
	_, missed := recoveryRoot(t, schedoapi.RequestAging)
	_, claimed := recoveryRoot(t, schedoapi.RequestAging)
	recoveryAttempt(t, claimed.ID, events.EventStatusNew, uuid.NewString())
	_, processing := recoveryRoot(t, schedoapi.Overdue)
	old := recoveryAttempt(t, processing.ID, events.EventStatusProcessing, uuid.NewString())
	_, accepted := recoveryRoot(t, schedoapi.EmailPullslips)
	acceptedAttempt := recoveryAttempt(t, accepted.ID, events.EventStatusProcessing, uuid.NewString())
	acceptedCtx := repo.WithBatchAttempt(appCtx, acceptedAttempt)
	require.NoError(t, schedRepo.RecordBatchEmailIntent(acceptedCtx, accepted.ID, []string{}, false))
	require.NoError(t, schedRepo.RecordBatchEmailAcceptance(acceptedCtx, accepted.ID))
	uncertainTask, uncertain := recoveryRoot(t, schedoapi.EmailPullslips)
	uncertainAttempt := recoveryAttempt(t, uncertain.ID, events.EventStatusProcessing, uuid.NewString())
	uncertainCtx := repo.WithBatchAttempt(appCtx, uncertainAttempt)
	require.NoError(t, schedRepo.RecordBatchEmailIntent(uncertainCtx, uncertain.ID, []string{}, false))
	_, oldVersion := recoveryRoot(t, schedoapi.RequestAging)
	unknown := recoveryAttempt(t, oldVersion.ID, events.EventStatusProcessing, "")
	_, live := recoveryRoot(t, schedoapi.RequestAging)
	liveLock := uuid.NewString()
	keeper, err := pgx.Connect(appCtx, connString)
	require.NoError(t, err)
	defer keeper.Close(appCtx)
	_, err = keeper.Exec(appCtx, "SELECT pg_advisory_lock(hashtextextended('broker:batch-worker:' || $1::text, 0))", liveLock)
	require.NoError(t, err)
	liveAttempt := recoveryAttempt(t, live.ID, events.EventStatusProcessing, liveLock)
	_, err = pool.Exec(appCtx, "UPDATE batch_action_run_event SET attempt_claimed_at = now() - interval '1 day' WHERE event_id = $1", live.ID)
	require.NoError(t, err)

	// A child that had entered work may already have delivered a notification.
	childTask, childRoot := recoveryRoot(t, schedoapi.Overdue)
	seedCtx, stopSeed := context.WithCancel(context.Background())
	defer stopSeed()
	seedBus := events.NewPostgresEventBus(eventRepo, connString)
	require.NoError(t, seedBus.Start(common.CreateExtCtxWithArgs(seedCtx, nil)))
	rootWorker := claimBatchWorker(t, eventRepo, seedBus, childRoot.ID)
	_, err = rootWorker.BeginTask(childRoot.ID, events.SignalObservers)
	require.NoError(t, err)
	childID, err := rootWorker.CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeBackgroundAction,
		childTask.ActionData, events.EventDomainPatronRequest, &childRoot.ID, events.SignalObservers)
	require.NoError(t, err)
	readyChildID, err := rootWorker.CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeBackgroundAction,
		childTask.ActionData, events.EventDomainPatronRequest, &childRoot.ID, events.SignalObservers)
	require.NoError(t, err)
	recoveryAttempt(t, readyChildID, events.EventStatusNew, uuid.NewString())
	childAttempt := recoveryAttempt(t, childID, events.EventStatusProcessing, uuid.NewString())
	_, err = pool.Exec(appCtx, "UPDATE batch_action_run_event SET work_started_at = now() WHERE event_id = $1", childID)
	require.NoError(t, err)
	// The background worker stopped after creating the inline action, before starting it.
	childWorker := events.WithWorkerContext(seedBus, repo.WithBatchAttempt(appCtx, childAttempt))
	inlineID, err := childWorker.CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeAction,
		events.EventData{}, events.EventDomainPatronRequest, &childID, events.SignalObservers)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pool.Exec(appCtx, "DELETE FROM event WHERE id = $1", inlineID)
		require.NoError(t, err)
	})
	_, err = rootWorker.CompleteTask(childRoot.ID, nil, events.EventStatusSuccess, events.SignalObservers)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := events.NewPostgresEventBus(eventRepo, connString)
	fenced := make(chan error, 1)
	run := func(ctx common.ExtendedContext, event events.Event) {
		_, _ = bus.ProcessTask(ctx, event, events.SignalObservers, func(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
			if event.ID == processing.ID {
				fenced <- schedRepo.SaveBatchActionPlan(repo.WithBatchAttempt(appCtx, old), event.ID, nil)
			}
			return events.EventStatusSuccess, nil
		})
	}
	bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, run)
	bus.HandleEventCreated(events.EventNameInvokeBackgroundAction, events.HandlerRoleConsumer, run)
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))

	waitForStatus := func(eventID string, status events.EventStatus, timeout time.Duration) {
		t.Helper()
		require.Eventually(t, func() bool {
			event, err := eventRepo.GetEvent(appCtx, eventID)
			return err == nil && event.EventStatus == status
		}, timeout, 20*time.Millisecond, eventID)
	}
	for _, event := range []events.Event{missed, claimed, processing, accepted} {
		waitForStatus(event.ID, events.EventStatusSuccess, 7*time.Second)
		var completedAt pgtype.Timestamptz
		require.NoError(t, pool.QueryRow(appCtx, "SELECT completed_at FROM batch_action_run WHERE id = $1", event.ID).Scan(&completedAt))
		require.True(t, completedAt.Valid, "finished recovery releases the reservation")
	}
	waitForStatus(readyChildID, events.EventStatusSuccess, 7*time.Second)
	require.ErrorIs(t, <-fenced, repo.ErrBatchAttemptLost)

	blocked := func(id, expectedReason string, attemptToKeep pgtype.Text, status events.EventStatus) {
		t.Helper()
		require.Eventually(t, func() bool {
			var reason, attempt pgtype.Text
			err := pool.QueryRow(appCtx, "SELECT recovery_reason, attempt_id FROM batch_action_run_event WHERE event_id = $1", id).
				Scan(&reason, &attempt)
			return err == nil && reason.Valid && reason.String == expectedReason && attempt == attemptToKeep
		}, 7*time.Second, 20*time.Millisecond)
		waitForStatus(id, status, time.Second)
	}
	blocked(uncertain.ID, "SMTP acceptance is unconfirmed", pgtype.Text{}, events.EventStatusProcessing)
	blocked(childID, "work may already have changed state or caused external effects", pgtype.Text{}, events.EventStatusProcessing)
	blocked(oldVersion.ID, "previous attempt has no verifiable worker session", pgtype.Text{String: unknown.ID, Valid: true}, events.EventStatusProcessing)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusConflict, requestBatchRun(handler, uncertainTask.ID).Code)

	var liveToken pgtype.Text
	require.NoError(t, pool.QueryRow(appCtx, "SELECT attempt_id FROM batch_action_run_event WHERE event_id = $1", live.ID).Scan(&liveToken))
	require.Equal(t, liveAttempt.ID, liveToken.String)
	waitForStatus(live.ID, events.EventStatusProcessing, time.Second)
	require.NoError(t, keeper.Close(appCtx))
	// No new notification is published for this event. The periodic sweep finds it.
	waitForStatus(live.ID, events.EventStatusSuccess, 20*time.Second)
	// If the child sorted before the parent at startup, the periodic sweep
	// sees the parent's committed recovery block.
	blocked(inlineID, "inline action requires reconciliation with its parent", pgtype.Text{}, events.EventStatusNew)
}

func batchTestBusDSN(t *testing.T, name string) string {
	t.Helper()
	dsn, err := url.Parse(connString)
	require.NoError(t, err)
	params := dsn.Query()
	params.Set("application_name", name)
	dsn.RawQuery = params.Encode()
	return dsn.String()
}

func TestBatchRecoveryOnListenerReconnect(t *testing.T) {
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool)
	_, startup := recoveryRoot(t, schedoapi.RequestAging)
	name := "batch_reconnect_" + uuid.NewString()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := events.NewPostgresEventBus(eventRepo, batchTestBusDSN(t, name))
	bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		_, _ = bus.ProcessTask(ctx, event, events.SignalObservers, func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
			return events.EventStatusSuccess, nil
		})
	})
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	done := func(id string) bool {
		event, err := eventRepo.GetEvent(appCtx, id)
		return err == nil && event.EventStatus == events.EventStatusSuccess
	}
	require.Eventually(t, func() bool { return done(startup.ID) }, 5*time.Second, 20*time.Millisecond)
	var pid int32
	require.NoError(t, pool.QueryRow(appCtx, "SELECT pid FROM pg_stat_activity WHERE application_name = $1 AND query = $2",
		name, "LISTEN "+events.EVENT_BUS_CHANNEL).Scan(&pid))
	var terminated bool
	require.NoError(t, pool.QueryRow(appCtx, "SELECT pg_terminate_backend($1)", pid).Scan(&terminated))
	require.True(t, terminated)
	// A connection from a different application publishes while the listener is down.
	_, missed := recoveryRoot(t, schedoapi.RequestAging)
	require.Eventually(t, func() bool { return done(missed.ID) }, 7*time.Second, 20*time.Millisecond)
}

func TestBatchRecoveryContinuesThroughLivePage(t *testing.T) {
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool)
	_, live := recoveryRoot(t, schedoapi.RequestAging)
	_, ready := recoveryRoot(t, schedoapi.RequestAging)
	keeper, err := pgx.Connect(appCtx, connString)
	require.NoError(t, err)
	defer keeper.Close(appCtx)
	lockID := uuid.NewString()
	_, err = keeper.Exec(appCtx, "SELECT pg_advisory_lock(hashtextextended('broker:batch-worker:' || $1::text, 0))", lockID)
	require.NoError(t, err)
	recoveryAttempt(t, live.ID, events.EventStatusProcessing, lockID)

	// Fill the first recovery page with children of the live run. Their IDs
	// sort before the ready root, and none will release a worker slot.
	const prefix = "00000000-0000-0000-0000-"
	prefixPattern := prefix + "%"
	t.Cleanup(func() {
		_, err := pool.Exec(appCtx, "DELETE FROM event WHERE id LIKE $1 AND parent_id = $2", prefixPattern, live.ID)
		require.NoError(t, err)
	})
	result, err := pool.Exec(appCtx, `
		WITH children AS (
			INSERT INTO event (id, timestamp, ill_transaction_id, parent_id, event_type, event_name, event_status, event_data, last_signal, patron_request_id)
			SELECT $3::text || lpad(n::text, 12, '0'), now(), ill_transaction_id, event.id, event_type, $4, 'PROCESSING', event_data, '', patron_request_id
			FROM event CROSS JOIN generate_series(1, 100) AS n
			WHERE event.id = $1
			RETURNING id
		)
		INSERT INTO batch_action_run_event (event_id, run_id, attempt_id, attempt_lock_id)
		SELECT id, $1, id, $2 FROM children
	`, live.ID, lockID, prefix, events.EventNameInvokeBackgroundAction)
	require.NoError(t, err)
	require.EqualValues(t, 100, result.RowsAffected())
	var earlierChildren int
	require.NoError(t, pool.QueryRow(appCtx, "SELECT count(*) FROM event WHERE parent_id = $1 AND id < $2", live.ID, ready.ID).Scan(&earlierChildren))
	require.Equal(t, 100, earlierChildren)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := events.NewPostgresEventBus(eventRepo, connString)
	run := func(ctx common.ExtendedContext, event events.Event) {
		_, _ = bus.ProcessTask(ctx, event, events.SignalObservers, func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
			return events.EventStatusSuccess, nil
		})
	}
	bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, run)
	bus.HandleEventCreated(events.EventNameInvokeBackgroundAction, events.HandlerRoleConsumer, run)
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	require.Eventually(t, func() bool {
		event, err := eventRepo.GetEvent(appCtx, ready.ID)
		return err == nil && event.EventStatus == events.EventStatusSuccess
	}, 7*time.Second, 20*time.Millisecond)
}

func TestBatchNotificationsSkipWorkerSessionsForProcessingEvent(t *testing.T) {
	_, running := recoveryRoot(t, schedoapi.RequestAging)
	config, err := pgxpool.ParseConfig(connString)
	require.NoError(t, err)
	// Use a single pooled connection so duplicate lookups do not grow the pool.
	config.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(appCtx, config)
	require.NoError(t, err)
	defer pool.Close()
	eventRepo := app.CreateEventRepo(pool)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := events.NewPostgresEventBus(eventRepo, connString)
	started := make(chan struct{})
	release := make(chan struct{}, 1)
	defer close(release)
	const duplicates = 12
	observed := make(chan struct{}, duplicates)
	bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleObserver, func(_ common.ExtendedContext, event events.Event) {
		if event.ID == running.ID {
			observed <- struct{}{}
		}
	})
	bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		_, _ = bus.ProcessTask(ctx, event, events.SignalObservers, func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
			if event.ID == running.ID {
				close(started)
				<-release
			}
			return events.EventStatusSuccess, nil
		})
	})
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not begin processing")
	}

	sessions := func() (int64, error) {
		var count int64
		err := pool.QueryRow(appCtx, "SELECT sessions FROM pg_stat_database WHERE datname = current_database()").Scan(&count)
		return count, err
	}
	before, err := sessions()
	require.NoError(t, err)
	for range duplicates {
		require.NoError(t, eventRepo.Notify(appCtx, running.ID, events.SignalTaskCreated, events.SignalAll))
	}
	deadline := time.After(5 * time.Second)
	for range duplicates {
		select {
		case <-observed:
		case <-deadline:
			t.Fatal("listener did not receive duplicate notifications")
		}
	}
	// Session statistics can lag while the listener and first worker are
	// still connected. Allow for those sessions to appear after the baseline.
	require.Never(t, func() bool {
		count, err := sessions()
		return err != nil || count-before > 2
	}, time.Second, 20*time.Millisecond, "duplicates opened dedicated worker sessions")
	release <- struct{}{}
	require.Eventually(t, func() bool {
		event, err := eventRepo.GetEvent(appCtx, running.ID)
		return err == nil && event.EventStatus == events.EventStatusSuccess
	}, 5*time.Second, 20*time.Millisecond)
}

func TestBatchRecoveryKeepsCanceledWorkersFencedByTheirSession(t *testing.T) {
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool)
	_, live := recoveryRoot(t, schedoapi.RequestAging)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	oldBus := events.NewPostgresEventBus(eventRepo, connString)
	started := make(chan repo.BatchAttempt, 1)
	release := make(chan struct{})
	defer close(release)
	oldBus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		if event.ID != live.ID {
			return
		}
		_, _ = oldBus.ProcessTask(ctx, event, events.SignalObservers, func(ctx common.ExtendedContext, _ events.Event) (events.EventStatus, *events.EventResult) {
			started <- repo.BatchAttemptFrom(ctx)
			<-release // An external call can outlive its canceled context.
			return events.EventStatusSuccess, nil
		})
	})
	require.NoError(t, oldBus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	var active repo.BatchAttempt
	select {
	case active = <-started:
		require.NotEmpty(t, active.LockID)
	case <-time.After(5 * time.Second):
		t.Fatal("old worker did not begin processing")
	}
	cancel()

	// A task without an owner proves that the second listener's startup recovery ran.
	_, pending := recoveryRoot(t, schedoapi.RequestAging)
	nextCtx, stopNext := context.WithCancel(context.Background())
	defer stopNext()
	nextBus := events.NewPostgresEventBus(eventRepo, connString)
	nextBus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		_, _ = nextBus.ProcessTask(ctx, event, events.SignalObservers, func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
			return events.EventStatusSuccess, nil
		})
	})
	require.NoError(t, nextBus.Start(common.CreateExtCtxWithArgs(nextCtx, nil)))
	require.Eventually(t, func() bool {
		event, err := eventRepo.GetEvent(appCtx, pending.ID)
		return err == nil && event.EventStatus == events.EventStatusSuccess
	}, 5*time.Second, 20*time.Millisecond)
	// The worker connection must still hold the advisory lock on another session.
	var available bool
	require.NoError(t, pool.QueryRow(appCtx, "SELECT pg_try_advisory_xact_lock(hashtextextended('broker:batch-worker:' || $1::text, 0))", active.LockID).Scan(&available))
	require.False(t, available)
	stored, err := eventRepo.GetEvent(appCtx, live.ID)
	require.NoError(t, err)
	require.Equal(t, events.EventStatusProcessing, stored.EventStatus)
	var attemptID pgtype.Text
	require.NoError(t, pool.QueryRow(appCtx, "SELECT attempt_id FROM batch_action_run_event WHERE event_id = $1", live.ID).Scan(&attemptID))
	require.Equal(t, active.ID, attemptID.String)
}

func TestBatchRecoveryLeavesActiveLegacyAttemptOwnedDuringUpgrade(t *testing.T) {
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	eventRepo := app.CreateEventRepo(pool).(*events.PgEventRepo)
	task, root := recoveryRoot(t, schedoapi.RequestAging)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	busCtx := common.CreateExtCtxWithArgs(ctx, nil)

	// The previous broker version claimed the signal without a session lock.
	// Drive a live handler with that persisted attempt while recovery runs.
	oldBus := events.NewPostgresEventBus(eventRepo, connString)
	require.NoError(t, oldBus.Start(busCtx))
	event, attempt, err := eventRepo.ClaimEventAttemptForSignal(busCtx, root.ID, events.SignalTaskCreated)
	require.NoError(t, err)
	require.NotEmpty(t, attempt.ID)
	require.Empty(t, attempt.LockID)
	started := make(chan struct{})
	release := make(chan struct{})
	progress := make(chan error, 1)
	completion := make(chan error, 1)
	go func() {
		oldCtx := repo.WithBatchAttempt(busCtx, attempt)
		_, err := oldBus.ProcessTask(oldCtx, event, events.SignalObservers, func(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
			close(started)
			select {
			case <-release:
				progress <- schedRepo.SaveBatchActionPlan(ctx, event.ID, nil)
			case <-ctx.Done():
				progress <- ctx.Err()
			}
			return events.EventStatusSuccess, nil
		})
		completion <- err
	}()
	select {
	case <-started:
	case err := <-completion:
		t.Fatalf("legacy worker exited before beginning: %v", err)
	case <-ctx.Done():
		t.Fatal("legacy worker did not begin processing")
	}

	recoveryBus := events.NewPostgresEventBus(app.CreateEventRepo(pool), connString)
	replayed := make(chan string, 1)
	recoveryBus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
		select {
		case replayed <- event.ID:
		default:
		}
	})
	require.NoError(t, recoveryBus.Start(busCtx))
	require.Eventually(t, func() bool {
		var reason, owner, lock pgtype.Text
		err := pool.QueryRow(busCtx, `
			SELECT recovery_reason, attempt_id, attempt_lock_id FROM batch_action_run_event
			WHERE event_id = $1 AND recovery_blocked_at IS NOT NULL
		`, root.ID).Scan(&reason, &owner, &lock)
		return err == nil && reason.String == "previous attempt has no verifiable worker session" &&
			owner.String == attempt.ID && !lock.Valid
	}, 5*time.Second, 20*time.Millisecond)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	require.Equal(t, http.StatusConflict, requestBatchRun(handler, task.ID).Code)

	close(release)
	for _, result := range []<-chan error{progress, completion} {
		select {
		case err := <-result:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Fatal("legacy worker did not finish")
		}
	}
	select {
	case id := <-replayed:
		t.Fatalf("recovery replayed legacy work: %s", id)
	default:
	}
	var completedAt pgtype.Timestamptz
	require.NoError(t, pool.QueryRow(busCtx, "SELECT completed_at FROM batch_action_run WHERE id = $1", root.ID).Scan(&completedAt))
	require.True(t, completedAt.Valid, "legacy worker completion releases the reservation")
	stored, err := eventRepo.GetEvent(busCtx, root.ID)
	require.NoError(t, err)
	require.Equal(t, events.EventStatusSuccess, stored.EventStatus)
}
