package sched_service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
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

func runSavedBatch(t *testing.T, taskID string, status int) {
	t.Helper()
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
	handler := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver(), nil)
	rr := httptest.NewRecorder()
	handler.PostBatchActionsIdRun(rr, httptest.NewRequest(http.MethodPost, "/batch_actions/"+taskID+"/run", nil), taskID, schedoapi.PostBatchActionsIdRunParams{})
	require.Equal(t, status, rr.Code, rr.Body.String())
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

func expireBatchLease(t *testing.T, eventID string) {
	t.Helper()
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	tag, err := pool.Exec(appCtx, "UPDATE batch_action_run_event SET lease_expires_at = clock_timestamp() - interval '1 second' WHERE event_id = $1 AND lease_expires_at IS NOT NULL", eventID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
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
	task := saveEmailBatchRunTask(t)

	// Queue before starting any listener. Startup must discover the durable task
	// after the original NOTIFY is gone.
	runSavedBatch(t, task.ID, http.StatusAccepted)
	started := make(chan events.Event, 1)
	first := startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
			started <- event
			// Simulate interruption between claim and completion.
		})
	})
	interrupted := receiveBatchEvent(t, started)
	require.Equal(t, events.EventStatusProcessing, interrupted.EventStatus)
	runSavedBatch(t, task.ID, http.StatusConflict)
	drainCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	require.NoError(t, first.DrainBatchWorkers(drainCtx))
	expireBatchLease(t, interrupted.ID)

	rerun := make(chan events.Event, 1)
	startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			rerun <- event
			_, _ = bus.ProcessTask(ctx, event, events.SignalConsumers, succeedBatchEvent)
		})
	})
	requireBatchEventStatus(t, interrupted.ID, events.EventStatusError)
	eventRepo := app.CreateEventRepo(schedRepo.(*sched_db.PgSchedRepo).Pool)
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
	task := saveEmailBatchRunTask(t)
	rootStarted := make(chan events.Event, 1)
	deadStarted := make(chan events.Event, 1)
	liveStarted := make(chan events.Event, 1)
	inlineCreated := make(chan string, 1)
	tryStaleWrite := make(chan struct{})
	finishSibling := make(chan struct{})
	staleResult := make(chan [2]error, 1)

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
			var staleWriteErr error
			_, completionErr := bus.ProcessTask(ctx, event, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				if e.EventData.Note == "slow" {
					liveStarted <- e
					select {
					case <-finishSibling:
					case <-ctx.Done():
					}
					return succeedBatchEvent(ctx, e)
				}
				deadStarted <- e
				scoped := events.WithWorkerContext(bus, ctx)
				inline, err := scoped.CreateTask(e.PatronRequestID, events.EventNameInvokeAction, e.EventData, events.EventDomainPatronRequest, &e.ID, events.SignalConsumers)
				if err != nil {
					return events.NewErrorResult("create test inline child", err.Error())
				}
				inlineCreated <- inline
				select {
				case <-tryStaleWrite:
					_, staleWriteErr = scoped.CreateTask(e.PatronRequestID, events.EventNameInvokeAction, e.EventData, events.EventDomainPatronRequest, &e.ID, events.SignalConsumers)
				case <-ctx.Done():
				}
				return succeedBatchEvent(ctx, e)
			})
			if event.EventData.Note == "expired" {
				staleResult <- [2]error{staleWriteErr, completionErr}
			}
		})
	}
	startBatchTestBus(t, register)
	t.Cleanup(func() {
		select {
		case <-finishSibling:
		default:
			close(finishSibling)
		}
		select {
		case <-tryStaleWrite:
		default:
			close(tryStaleWrite)
		}
	})
	runSavedBatch(t, task.ID, http.StatusAccepted)
	root := receiveBatchEvent(t, rootStarted)
	dead := receiveBatchEvent(t, deadStarted)
	live := receiveBatchEvent(t, liveStarted)
	var inline string
	select {
	case inline = <-inlineCreated:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not create its inline child")
	}
	requireBatchEventStatus(t, root.ID, events.EventStatusSuccess)
	runSavedBatch(t, task.ID, http.StatusConflict)
	expireBatchLease(t, dead.ID)

	// A second replica recovers just the expired worker and its inline child.
	startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, e events.Event) {
			_, _ = bus.ProcessTask(ctx, e, events.SignalConsumers, succeedBatchEvent)
		})
	})
	requireBatchEventStatus(t, dead.ID, events.EventStatusError)
	requireBatchEventStatus(t, inline, events.EventStatusError)
	runSavedBatch(t, task.ID, http.StatusConflict)
	requireBatchEventStatus(t, live.ID, events.EventStatusProcessing)

	close(tryStaleWrite)
	select {
	case errs := <-staleResult:
		require.ErrorIs(t, errs[0], repo.ErrBatchLeaseLost)
		require.ErrorIs(t, errs[1], repo.ErrBatchLeaseLost)
	case <-time.After(5 * time.Second):
		t.Fatal("stale worker did not return")
	}
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
	resume := make(chan struct{})
	first := startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		handler := prservice.CreatePatronRequestMessageHandler(requestRepo, eventRepo, nil, bus)
		handler.SetAutoActionRunner(batchAutoActionRunner(func(ctx common.ExtendedContext, pr pr_db.PatronRequest, parentID *string) error {
			id, err := events.WithWorkerContext(bus, ctx).CreateTask(pr.ID, events.EventNameInvokeAction, events.EventData{}, events.EventDomainPatronRequest, parentID, events.SignalConsumers)
			if err != nil {
				return err
			}
			_, err = bus.ProcessExclusiveTask(ctx, events.Event{ID: id}, events.SignalConsumers, func(ctx common.ExtendedContext, e events.Event) (events.EventStatus, *events.EventResult) {
				autoActionStarted <- e
				select {
				case <-resume:
				case <-ctx.Done():
				}
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
	t.Cleanup(func() {
		select {
		case <-resume:
		default:
			close(resume)
		}
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
	expireBatchLease(t, worker.ID)
	close(resume)
	drainCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	require.NoError(t, first.DrainBatchWorkers(drainCtx))

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
	t.Cleanup(func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	})
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

	drainCtx, stop := context.WithCancel(context.Background())
	stop()
	require.ErrorIs(t, first.DrainBatchWorkers(drainCtx), context.Canceled)
	close(finish)

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
	claimed := make(chan events.Event, 1)
	startBatchTestBus(t, func(bus *events.PostgresEventBus) {
		bus.HandleEventCreated(events.EventNameInvokeBatchAction, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
			claimed <- event
		})
	})
	runSavedBatch(t, task.ID, http.StatusAccepted)
	event := receiveBatchEvent(t, claimed)
	pool := schedRepo.(*sched_db.PgSchedRepo).Pool
	var capAt time.Time
	require.NoError(t, pool.QueryRow(appCtx, "UPDATE batch_action_run_event SET lease_expires_at = clock_timestamp() + interval '30 seconds', lease_cap_at = clock_timestamp() + interval '1 minute' WHERE event_id = $1 RETURNING lease_cap_at", event.ID).Scan(&capAt))
	queries := events.Queries{}
	params := events.RenewBatchWorkerLeaseParams{EventID: event.ID, LeaseDuration: pgtype.Interval{Microseconds: (5 * time.Minute).Microseconds(), Valid: true}}
	renewed, err := queries.RenewBatchWorkerLease(appCtx, pool, params)
	require.NoError(t, err)
	require.True(t, capAt.Equal(renewed.LeaseExpiresAt.Time))

	expireBatchLease(t, event.ID)
	_, err = queries.RenewBatchWorkerLease(appCtx, pool, params)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}
