package sched_service

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/client"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/repo"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func runSavedBatch(t *testing.T, taskID string, status int) string {
	t.Helper()
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	rr := httptest.NewRecorder()
	handler.PostBatchActionsIdRun(rr, httptest.NewRequest(http.MethodPost, "/batch_actions/"+taskID+"/run", nil), taskID, schedoapi.PostBatchActionsIdRunParams{})
	require.Equal(t, status, rr.Code, rr.Body.String())
	return rr.Body.String()
}

func startBatchTestBus(t *testing.T, register func(*events.PostgresEventBus)) *events.PostgresEventBus {
	t.Helper()
	e := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	bus := events.NewPostgresEventBus(e, connString)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	register(bus)
	require.NoError(t, bus.Start(common.CreateExtCtxWithArgs(ctx, nil)))
	t.Cleanup(func() {
		deadline, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		require.NoError(t, bus.DrainBatchWorkers(deadline))
	})
	return bus
}

func waitForBatchLeaseExpiry(t *testing.T, eventID string) {
	t.Helper()
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	require.Eventually(t, func() bool {
		var expired bool
		err := pool.QueryRow(appCtx, "SELECT lease_expires_at <= clock_timestamp() FROM batch_action_run_event WHERE event_id = $1", eventID).Scan(&expired)
		return err == nil && expired
	}, 5*time.Second, 10*time.Millisecond)
}

func requireBatchEventStatus(t *testing.T, id string, status events.EventStatus) {
	t.Helper()
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	require.Eventually(t, func() bool {
		event, err := eventRepo.GetEvent(appCtx, id)
		return err == nil && event.EventStatus == status
	}, 5*time.Second, 10*time.Millisecond)
}

func receiveBatchEvent(t *testing.T, started <-chan events.Event) events.Event {
	t.Helper()
	select {
	case event := <-started:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("batch worker did not receive an event")
		return events.Event{}
	}
}

func TestInterruptedBatchRequiresManualRerunAfterRestart(t *testing.T) {
	t.Setenv("BATCH_WORKER_LEASE", "1s")
	task := saveEmailBatchRunTask(t)

	// Queue before starting any listener. Startup must discover the durable task
	// after the original NOTIFY is gone.
	runSavedBatch(t, task.ID, http.StatusAccepted)
	started := make(chan events.Event, 1)
	type protocolTasks struct {
		requester, supplier string
		err                 error
	}
	delegated := make(chan protocolTasks, 1)
	first := startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			started <- event
			scoped := events.WithWorkerContext(bus, ctx)
			requester, err := scoped.CreateTask(events.DEFAULT_ILL_TRANSACTION_ID, events.EventNameMessageRequester, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalObservers)
			if err != nil {
				delegated <- protocolTasks{err: err}
				return
			}
			supplier, err := scoped.CreateTask(events.DEFAULT_ILL_TRANSACTION_ID, events.EventNameMessageSupplier, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalObservers)
			if err == nil {
				_, err = scoped.BeginTask(supplier, events.SignalObservers)
			}
			delegated <- protocolTasks{requester: requester, supplier: supplier, err: err}
			// Simulate interruption between claim and completion.
		})
	})
	interrupted := receiveBatchEvent(t, started)
	require.Equal(t, events.EventStatusProcessing, interrupted.EventStatus)
	body := runSavedBatch(t, task.ID, http.StatusConflict)
	require.Contains(t, body, interrupted.ID)
	require.Contains(t, body, interrupted.Timestamp.Time.UTC().Format(time.RFC3339Nano))
	drainCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	require.NoError(t, first.DrainBatchWorkers(drainCtx))
	tasks := <-delegated
	require.NoError(t, tasks.err)
	waitForBatchLeaseExpiry(t, interrupted.ID)

	rerun := make(chan events.Event, 1)
	completedProtocol := make(chan events.Event, 2)
	startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		for _, name := range []events.EventName{events.EventNameMessageRequester, events.EventNameMessageSupplier} {
			bus.HandleTaskCompleted(name, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
				completedProtocol <- event
			})
		}
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			rerun <- event
			_, _ = bus.ProcessTask(ctx, event, events.SignalConsumers, succeedBatchEvent)
		})
	})
	requireBatchEventStatus(t, interrupted.ID, events.EventStatusError)
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	for _, id := range []string{tasks.requester, tasks.supplier} {
		event, err := eventRepo.GetEvent(appCtx, id)
		require.NoError(t, err)
		require.Equal(t, events.EventStatusError, event.EventStatus)
		require.Empty(t, event.LastSignal)
		require.NotNil(t, event.ResultData.EventError)
	}
	select {
	case event := <-completedProtocol:
		t.Fatalf("abandoned protocol task %s emitted a completion signal", event.ID)
	case <-time.After(100 * time.Millisecond):
	}
	history, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, history, 1, "recovery only records the interruption")
	runSavedBatch(t, task.ID, http.StatusAccepted)
	manual := receiveBatchEvent(t, rerun)
	require.NotEqual(t, interrupted.ID, manual.ID)
	requireBatchEventStatus(t, manual.ID, events.EventStatusSuccess)
	saved, err := schedRepo.GetScheduledTaskById(appCtx, task.ID, nil)
	require.NoError(t, err)
	require.Equal(t, task, saved)
}

func succeedBatchEvent(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
	return events.EventStatusSuccess, nil
}

func TestExpiredChildIsFencedWhileSiblingFinishes(t *testing.T) {
	t.Setenv("BATCH_WORKER_LEASE", "1s")
	task := saveEmailBatchRunTask(t)
	rootStarted := make(chan events.Event, 1)
	deadStarted := make(chan events.Event, 1)
	liveStarted := make(chan events.Event, 1)
	type interruptedChild struct {
		inline string
		ctx    common.ExtendedContext
		err    error
	}
	inlineCreated := make(chan interruptedChild, 1)
	finishSibling := make(chan struct{})

	register := func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			rootStarted <- event
			_, _ = bus.ProcessTask(ctx, event, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				for _, note := range []string{"expired", "slow"} {
					data := e.EventData
					data.Note = note
					_, err := events.WithWorkerContext(bus, ctx).CreateTask(events.DEFAULT_PATRON_REQUEST_ID, events.EventNameInvokeBackgroundAction, data, events.EventDomainPatronRequest, &e.ID, events.SignalConsumers)
					if err != nil {
						return events.NewErrorResult("create test child", err.Error())
					}
				}
				return succeedBatchEvent(ctx, e)
			})
		})
		bus.HandleEventCreated(events.EventNameInvokeBackgroundAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			if event.EventData.Note == "expired" {
				deadStarted <- event
				scoped := events.WithWorkerContext(bus, ctx)
				inline, err := scoped.CreateTask(event.PatronRequestID, events.EventNameInvokeAction, event.EventData, events.EventDomainPatronRequest, &event.ID, events.SignalConsumers)
				args := ctx.LoggerArgs()
				staleCtx := common.CreateExtCtxWithArgs(context.WithoutCancel(ctx), &args)
				inlineCreated <- interruptedChild{inline: inline, ctx: staleCtx, err: err}
				return
			}
			_, _ = bus.ProcessTask(ctx, event, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				liveStarted <- e
				select {
				case <-finishSibling:
				case <-ctx.Done():
				}
				return succeedBatchEvent(ctx, e)
			})
		})
	}
	first := startBatchTestBus(t, register)
	t.Cleanup(func() {
		select {
		case <-finishSibling:
		default:
			close(finishSibling)
		}
	})
	runSavedBatch(t, task.ID, http.StatusAccepted)
	root := receiveBatchEvent(t, rootStarted)
	dead := receiveBatchEvent(t, deadStarted)
	live := receiveBatchEvent(t, liveStarted)
	var child interruptedChild
	select {
	case child = <-inlineCreated:
		require.NoError(t, child.err)
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not create its inline child")
	}
	requireBatchEventStatus(t, root.ID, events.EventStatusSuccess)
	runSavedBatch(t, task.ID, http.StatusConflict)
	waitForBatchLeaseExpiry(t, dead.ID)

	// A second replica recovers just the expired worker and its inline child.
	startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, e events.Event) {
			_, _ = bus.ProcessTask(ctx, e, events.SignalConsumers, succeedBatchEvent)
		})
	})
	requireBatchEventStatus(t, dead.ID, events.EventStatusError)
	requireBatchEventStatus(t, child.inline, events.EventStatusError)
	runSavedBatch(t, task.ID, http.StatusConflict)
	requireBatchEventStatus(t, live.ID, events.EventStatusProcessing)

	scoped := events.WithWorkerContext(first, child.ctx)
	_, err := scoped.CreateTask(dead.PatronRequestID, events.EventNameInvokeAction, dead.EventData, events.EventDomainPatronRequest, &dead.ID, events.SignalConsumers)
	require.ErrorIs(t, err, repo.ErrBatchLeaseLost)
	_, err = scoped.CompleteTask(dead.ID, nil, events.EventStatusSuccess, events.SignalConsumers)
	require.ErrorIs(t, err, repo.ErrBatchLeaseLost)
	requireBatchEventStatus(t, dead.ID, events.EventStatusError)
	close(finishSibling)
	requireBatchEventStatus(t, live.ID, events.EventStatusSuccess)

	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	history, err := eventRepo.GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, history, 3, "no automatic replay of the parent or children")
}

type batchAutoActionRunner func(common.ExtendedContext, pr_db.PatronRequest, *string) error

func (run batchAutoActionRunner) RunAutoActionsOnStateEntry(ctx common.ExtendedContext, pr pr_db.PatronRequest, parentID *string, _ string) error {
	return run(ctx, pr, parentID)
}

func TestInterruptedLocalMessageAndAutoActionReleaseBatchForManualRerun(t *testing.T) {
	t.Setenv("BATCH_WORKER_LEASE", "1s")
	task := saveEmailBatchRunTask(t)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	requestRepo := pr_db.CreatePrRepo(pool, false)
	eventRepo := app.CreateEventRepo(pool)
	request, err := requestRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: uuid.NewString(), Side: prservice.SideBorrowing, State: prservice.BorrowerStateSent,
		StateModel: "default", Language: "english", Items: []pr_db.PrItem{},
		CreatedAt: pgtype.Timestamp{Time: time.Now().UTC(), Valid: true},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, requestRepo.DeletePatronRequest(appCtx, request.ID)) })

	workerStarted := make(chan events.Event, 1)
	autoActionStarted := make(chan events.Event, 1)
	first := startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		handler := prservice.CreatePatronRequestMessageHandler(requestRepo, eventRepo, nil, bus)
		handler.SetAutoActionRunner(batchAutoActionRunner(func(ctx common.ExtendedContext, pr pr_db.PatronRequest, parentID *string) error {
			id, err := events.WithWorkerContext(bus, ctx).CreateTask(pr.ID, events.EventNameInvokeAction, events.EventData{}, events.EventDomainPatronRequest, parentID, events.SignalConsumers)
			if err != nil {
				return err
			}
			_, err = bus.ProcessExclusiveTask(ctx, events.Event{ID: id}, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				autoActionStarted <- e
				<-ctx.Done()
				return succeedBatchEvent(ctx, e)
			})
			return err
		}))
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, e events.Event) {
			workerStarted <- e
			_, _ = bus.ProcessTask(ctx, e, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				msg := iso18626.NewISO18626Message()
				msg.SupplyingAgencyMessage = &iso18626.SupplyingAgencyMessage{
					Header:      iso18626.Header{RequestingAgencyRequestId: request.ID},
					MessageInfo: iso18626.MessageInfo{ReasonForMessage: iso18626.TypeReasonForMessageStatusChange},
					StatusInfo:  iso18626.StatusInfo{Status: iso18626.TypeStatusWillSupply},
				}
				if _, err := handler.HandleMessage(ctx, msg, nil); err != nil {
					return events.NewErrorResult("local message failed", err.Error())
				}
				return succeedBatchEvent(ctx, e)
			})
		})
	})
	runSavedBatch(t, task.ID, http.StatusAccepted)
	worker := receiveBatchEvent(t, workerStarted)
	autoAction := receiveBatchEvent(t, autoActionStarted)
	require.True(t, autoAction.ParentID.Valid)
	message, err := eventRepo.GetEvent(appCtx, autoAction.ParentID.String)
	require.NoError(t, err)
	require.Equal(t, events.EventNamePatronRequestMessage, message.EventName)
	require.Equal(t, events.EventStatusProcessing, message.EventStatus)
	_, err = first.CompleteTask(message.ID, nil, events.EventStatusSuccess, events.SignalConsumers)
	require.ErrorIs(t, err, repo.ErrBatchLeaseLost)
	drainCtx, stop := context.WithCancel(context.Background())
	stop()
	require.ErrorIs(t, first.DrainBatchWorkers(drainCtx), context.Canceled)
	waitForBatchLeaseExpiry(t, worker.ID)

	rerun := make(chan events.Event, 1)
	startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, e events.Event) {
			rerun <- e
			_, _ = bus.ProcessTask(ctx, e, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				id, err := events.WithWorkerContext(bus, ctx).CreateTask(request.ID, events.EventNameInvokeAction, events.EventData{}, events.EventDomainPatronRequest, &e.ID, events.SignalConsumers)
				if err != nil {
					return events.NewErrorResult("create replacement action", err.Error())
				}
				next, err := bus.ProcessExclusiveTask(ctx, events.Event{ID: id}, events.SignalConsumers, succeedBatchEvent)
				if err != nil {
					return events.NewErrorResult("process replacement action", err.Error())
				}
				return next.EventStatus, &next.ResultData
			})
		})
	})
	for _, id := range []string{worker.ID, message.ID, autoAction.ID} {
		requireBatchEventStatus(t, id, events.EventStatusError)
	}
	runSavedBatch(t, task.ID, http.StatusAccepted)
	requireBatchEventStatus(t, receiveBatchEvent(t, rerun).ID, events.EventStatusSuccess)
}

func TestBatchWorkersLeaveQueuedWorkForTheNextReplicaWhenDraining(t *testing.T) {
	const workers = 8
	queued := make([]sched_db.ScheduledTask, workers+2)
	for i := range queued {
		queued[i] = saveBatchRunTask(t, fmt.Sprintf("ISIL:CAPACITY-%d", i), schedoapi.EmailPullslips)
		runSavedBatch(t, queued[i].ID, http.StatusAccepted)
	}

	started := make(chan events.Event, len(queued))
	finish := make(chan struct{})
	release := sync.OnceFunc(func() { close(finish) })
	first := startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, e events.Event) {
			_, _ = bus.ProcessTask(ctx, e, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				started <- e
				select {
				case <-ctx.Done():
				case <-finish:
				}
				return succeedBatchEvent(ctx, e)
			})
		})
	})
	t.Cleanup(release)
	claimed := make(map[string]bool)
	for range workers {
		event := receiveBatchEvent(t, started)
		require.False(t, claimed[event.ID], "a task is claimed once")
		claimed[event.ID] = true
	}
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	var processing, pending int
	require.NoError(t, pool.QueryRow(appCtx, "SELECT count(*) FILTER (WHERE lease_expires_at IS NOT NULL), count(*) FILTER (WHERE ready_at IS NOT NULL) FROM batch_action_run_event").Scan(&processing, &pending))
	require.Equal(t, workers, processing)
	require.Equal(t, len(queued)-workers, pending)

	drainCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	timer := time.AfterFunc(50*time.Millisecond, release)
	defer timer.Stop()
	require.NoError(t, first.DrainBatchWorkers(drainCtx))
	for id := range claimed {
		requireBatchEventStatus(t, id, events.EventStatusSuccess)
	}

	startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			started <- event
			_, _ = bus.ProcessTask(ctx, event, events.SignalConsumers, succeedBatchEvent)
		})
	})
	for range len(queued) - workers {
		event := receiveBatchEvent(t, started)
		require.False(t, claimed[event.ID])
		requireBatchEventStatus(t, event.ID, events.EventStatusSuccess)
	}
}

func TestBatchLeaseRenewalStaysWithinTheHardCap(t *testing.T) {
	task := saveEmailBatchRunTask(t)
	runSavedBatch(t, task.ID, http.StatusAccepted)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	history, err := app.CreateEventRepo(pool).GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	event := history[0]
	queries := events.Queries{}
	initial, err := queries.StartBatchWorkerLease(appCtx, pool, events.StartBatchWorkerLeaseParams{
		EventID: event.ID, LeaseDuration: pgtype.Interval{Microseconds: time.Second.Microseconds(), Valid: true},
		LeaseCap: pgtype.Interval{Microseconds: (1500 * time.Millisecond).Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	var capAt time.Time
	require.NoError(t, pool.QueryRow(appCtx, "SELECT lease_cap_at FROM batch_action_run_event WHERE event_id = $1", event.ID).Scan(&capAt))
	require.True(t, initial.LeaseExpiresAt.Time.Before(capAt))
	params := events.RenewBatchWorkerLeaseParams{EventID: event.ID, LeaseDuration: pgtype.Interval{Microseconds: time.Minute.Microseconds(), Valid: true}}
	renewed, err := queries.RenewBatchWorkerLease(appCtx, pool, params)
	require.NoError(t, err)
	require.True(t, capAt.Equal(renewed.LeaseExpiresAt.Time))

	waitForBatchLeaseExpiry(t, event.ID)
	_, err = queries.RenewBatchWorkerLease(appCtx, pool, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestDelegatedBatchWorkFollowsRenewalAcrossReplicas(t *testing.T) {
	task := saveEmailBatchRunTask(t)
	runSavedBatch(t, task.ID, http.StatusAccepted)
	ownerPool, err := app.InitDbPool()
	require.NoError(t, err)
	t.Cleanup(ownerPool.Close)
	history, err := app.CreateEventRepo(ownerPool).GetBatchActionEvents(appCtx, task.ID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	root := history[0]

	reply := make(chan struct{})
	sendStarted := make(chan struct{}, 1)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sendStarted <- struct{}{}
		select {
		case <-reply:
			w.Header().Set("Content-Type", "application/xml")
			_ = xml.NewEncoder(w).Encode(iso18626.NewISO18626Message())
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(peer.Close)
	isoClient := client.CreateIso18626Client(nil, nil, prservice.PatronRequestMessageHandler{}, 1<<20, 0)
	sent := make(chan error, 1)
	waiting := make(chan events.Event, 1)
	expired := make(chan error, 1)
	replica := startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameMessageRequester, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			completed, err := bus.ProcessTask(ctx, event, events.SignalObservers, func(ctx common.ExtendedContext, event events.Event) (events.EventStatus, *events.EventResult) {
				if _, err := isoClient.HandleIllMessage(ctx, &ill_db.Peer{Url: peer.URL}, iso18626.NewISO18626Message()); err != nil {
					return events.NewErrorResult("send test ISO message", err.Error())
				}
				return succeedBatchEvent(ctx, event)
			})
			if err == nil && completed.EventStatus != events.EventStatusSuccess {
				err = fmt.Errorf("delegated ISO send ended with %s: %+v", completed.EventStatus, completed.ResultData.EventError)
			}
			sent <- err
		})
		bus.HandleEventCreated(events.EventNameMessageSupplier, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			waiting <- event
			<-ctx.Done()
			expired <- ctx.Err()
		})
	})

	// Claim through a separate pool. The consuming replica only sees the
	// durable lease, including changes made after the tasks have started.
	queries := events.Queries{}
	initial, err := queries.StartBatchWorkerLease(appCtx, ownerPool, events.StartBatchWorkerLeaseParams{
		EventID: root.ID, LeaseDuration: pgtype.Interval{Microseconds: time.Second.Microseconds(), Valid: true},
		LeaseCap: pgtype.Interval{Microseconds: (3 * time.Second).Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	_, err = queries.BeginBatchWorkerEvent(appCtx, ownerPool, root.ID)
	require.NoError(t, err)
	owner := repo.WithBatchWorker(appCtx, root.ID, func() time.Time { return initial.LeaseExpiresAt.Time })
	bus := events.WithWorkerContext(replica, owner)
	for _, name := range []events.EventName{events.EventNameMessageSupplier, events.EventNameMessageRequester} {
		id, err := bus.CreateTask(events.DEFAULT_ILL_TRANSACTION_ID, name, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, queries.DeleteEvent(appCtx, ownerPool, id)) })
	}
	receiveBatchEvent(t, waiting)
	select {
	case <-sendStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("delegated ISO send did not start")
	}
	renewed, err := queries.RenewBatchWorkerLease(appCtx, ownerPool, events.RenewBatchWorkerLeaseParams{
		EventID: root.ID, LeaseDuration: pgtype.Interval{Microseconds: time.Minute.Microseconds(), Valid: true},
	})
	require.NoError(t, err)
	var capAt time.Time
	require.NoError(t, ownerPool.QueryRow(appCtx, "SELECT lease_cap_at FROM batch_action_run_event WHERE event_id = $1", root.ID).Scan(&capAt))
	require.True(t, renewed.LeaseExpiresAt.Time.Equal(capAt))
	require.Eventually(t, func() bool {
		var passed bool
		err := ownerPool.QueryRow(appCtx, "SELECT clock_timestamp() >= $1::timestamptz", initial.LeaseExpiresAt.Time).Scan(&passed)
		return err == nil && passed
	}, 2*time.Second, 10*time.Millisecond)
	close(reply)
	select {
	case err := <-sent:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("delegated ISO send did not complete")
	}
	select {
	case err := <-expired:
		t.Fatalf("delegated task stopped before the renewed lease expired: %v", err)
	default:
	}
	select {
	case err := <-expired:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(4 * time.Second):
		t.Fatal("delegated task outlived the hard cap")
	}
	waitForBatchLeaseExpiry(t, root.ID)
}
