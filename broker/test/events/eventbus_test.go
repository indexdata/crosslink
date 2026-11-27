package events

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/dbutil"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

var eventBus events.EventBus
var illRepo ill_db.IllRepo
var eventRepo events.EventRepo

func TestMain(m *testing.M) {
	ctx := context.Background()
	app.DB_PROVISION = true

	pgContainer, err := testutil.RunPostgres(ctx)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")
	app.ConnectionString = connStr

	fmt.Print("Postgres connection string: ", connStr)
	app.MigrationsFolder = "file://../../migrations"
	err = app.RunDbUp()
	test.Expect(err, "failed to run migrations")

	dbPool, err := dbutil.InitDbPool(connStr)
	test.Expect(err, "failed to init db pool")

	eventRepo = app.CreateEventRepo(dbPool)
	eventBus = app.CreateEventBus(eventRepo)
	illRepo = ill_db.CreateIllRepo(dbPool)
	err = app.StartEventBus(ctx, eventBus)
	test.Expect(err, "failed to start event bus")

	code := m.Run()

	test.Expect(test.TerminatePGContainer(ctx, pgContainer), "failed to stop db container")
	os.Exit(code)
}

func TestMultipleEventHandlers(t *testing.T) {
	noPools := 3
	noEvents := 2
	receivedAr := make([][]events.Event, noPools)
	var receivedMu sync.Mutex
	var normalCreated atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < noPools; i++ {
		dbPool, err := dbutil.InitDbPool(app.ConnectionString)
		assert.NoError(t, err, "failed to init db pool")
		defer dbPool.Close()

		eventRepo := app.CreateEventRepo(dbPool)
		eventBus := app.CreateEventBus(eventRepo)

		eventBus.HandleEventCreated(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			receivedMu.Lock()
			defer receivedMu.Unlock()
			receivedAr[i] = append(receivedAr[i], event)
		})
		eventBus.HandleEventCreated(events.EventNameConfirmRequesterMsg, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			normalCreated.Add(1)
		})
		err = app.StartEventBus(ctx, eventBus)
		assert.NoError(t, err, "failed to start event bus")
	}
	// Registered after the pool defers so it runs first (LIFO), cancelling the
	// event bus goroutines before their pools are closed.
	defer func() { cancel(); time.Sleep(50 * time.Millisecond) }()

	var requestReceived1 []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		requestReceived1 = append(requestReceived1, event)
	})

	for i := 0; i < noEvents; i++ {
		illId := apptest.GetIllTransId(t, illRepo)
		_, err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
		assert.NoError(t, err, "Task should be created without errors")
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		total := len(requestReceived1)
		for i := 0; i < noPools; i++ {
			total += len(receivedAr[i])
		}
		return total >= noEvents
	}) {
		t.Error("Expected to have some events")
	}
	receivedMu.Lock()
	total := len(requestReceived1)
	for i := 0; i < noPools; i++ {
		total += len(receivedAr[i])
	}
	receivedMu.Unlock()
	assert.Equal(t, noEvents, total, "Total number of events should match the number of created tasks")
	if total != noEvents {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		for e := range requestReceived1 {
			t.Logf("Request event %d: %s", e, requestReceived1[e].ID)
		}
		for i := 0; i < noPools; i++ {
			for e := range receivedAr[i] {
				t.Logf("Received event %d from pool %d: %s", e, i, receivedAr[i][e].ID)
			}
		}
	}
}

func TestBroadcastEventHandlers(t *testing.T) {
	noPools := 3
	noEvents := 2
	receivedAr := make([][]events.Event, noPools)
	var receivedMu sync.Mutex
	var normalCreated atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	for i := 0; i < noPools; i++ {
		dbPool, err := dbutil.InitDbPool(app.ConnectionString)
		assert.NoError(t, err, "failed to init db pool")
		defer dbPool.Close()

		eventRepo := app.CreateEventRepo(dbPool)
		eventBus := app.CreateEventBus(eventRepo)

		eventBus.HandleEventCreated(events.EventNameConfirmRequesterMsg, events.HandlerRoleObserver, func(ctx common.ExtendedContext, event events.Event) {
			receivedMu.Lock()
			defer receivedMu.Unlock()
			receivedAr[i] = append(receivedAr[i], event)
		})
		eventBus.HandleEventCreated(events.EventNameConfirmRequesterMsg, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			normalCreated.Add(1)
		})
		err = app.StartEventBus(ctx, eventBus)
		assert.NoError(t, err, "failed to start event bus")
	}
	// Registered after the pool defers so it runs first (LIFO), cancelling the
	// event bus goroutines before their pools are closed.
	defer func() { cancel(); time.Sleep(50 * time.Millisecond) }()

	var requestReceived1 []events.Event
	eventBus.HandleEventCreated(events.EventNameConfirmRequesterMsg, events.HandlerRoleObserver, func(ctx common.ExtendedContext, event events.Event) {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		requestReceived1 = append(requestReceived1, event)
	})
	eventBus.HandleEventCreated(events.EventNameConfirmRequesterMsg, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		normalCreated.Add(1)
	})

	for i := 0; i < noEvents; i++ {
		illId := apptest.GetIllTransId(t, illRepo)
		_, err := eventBus.CreateTask(illId, events.EventNameConfirmRequesterMsg, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalObservers)
		assert.NoError(t, err, "Task should be created without errors")
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		total := len(requestReceived1)
		for i := 0; i < noPools; i++ {
			total += len(receivedAr[i])
		}
		return total >= noEvents*(noPools+1) // +1 for the main event bus
	}) {
		t.Error("Expected to have some events")
	}
	receivedMu.Lock()
	total := len(requestReceived1)
	for i := 0; i < noPools; i++ {
		total += len(receivedAr[i])
	}
	receivedMu.Unlock()
	assert.Equal(t, noEvents*(noPools+1), total, "Total number of events should match the number of created tasks")
	assert.Equal(t, int32(0), normalCreated.Load(), "broadcast-created signals should not invoke normal created handlers")
	if total != noEvents*(noPools+1) {
		receivedMu.Lock()
		defer receivedMu.Unlock()
		for e := range requestReceived1 {
			t.Logf("Request event %d: %s", e, requestReceived1[e].ID)
		}
		for i := 0; i < noPools; i++ {
			for e := range receivedAr[i] {
				t.Logf("Received event %d from pool %d: %s", e, i, receivedAr[i][e].ID)
			}
		}
	}
}

func TestBroadcastTaskLifecycleHandlersDoNotDuplicateCoreHandlers(t *testing.T) {
	noPools := 3
	noBuses := int32(noPools + 1) // +1 for the main event bus
	var coreStarted atomic.Int32
	var coreCompleted atomic.Int32
	var broadcastStarted atomic.Int32
	var broadcastCompleted atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())

	for i := 0; i < noPools; i++ {
		dbPool, err := dbutil.InitDbPool(app.ConnectionString)
		assert.NoError(t, err, "failed to init db pool")
		defer dbPool.Close()

		eventRepo := app.CreateEventRepo(dbPool)
		eventBus := app.CreateEventBus(eventRepo)

		eventBus.HandleTaskStarted(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			coreStarted.Add(1)
		})
		eventBus.HandleTaskCompleted(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
			coreCompleted.Add(1)
		})
		eventBus.HandleTaskStarted(events.EventNameRequestReceived, events.HandlerRoleObserver, func(ctx common.ExtendedContext, event events.Event) {
			broadcastStarted.Add(1)
		})
		eventBus.HandleTaskCompleted(events.EventNameRequestReceived, events.HandlerRoleObserver, func(ctx common.ExtendedContext, event events.Event) {
			broadcastCompleted.Add(1)
		})
		err = app.StartEventBus(ctx, eventBus)
		assert.NoError(t, err, "failed to start event bus")
	}
	// Registered after the pool defers so it runs first (LIFO), cancelling the
	// event bus goroutines before their pools are closed.
	defer func() { cancel(); time.Sleep(50 * time.Millisecond) }()

	eventBus.HandleTaskStarted(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		coreStarted.Add(1)
	})
	eventBus.HandleTaskCompleted(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		coreCompleted.Add(1)
	})
	eventBus.HandleTaskStarted(events.EventNameRequestReceived, events.HandlerRoleObserver, func(ctx common.ExtendedContext, event events.Event) {
		broadcastStarted.Add(1)
	})
	eventBus.HandleTaskCompleted(events.EventNameRequestReceived, events.HandlerRoleObserver, func(ctx common.ExtendedContext, event events.Event) {
		broadcastCompleted.Add(1)
	})

	illId := apptest.GetIllTransId(t, illRepo)
	eventId, err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
	assert.NoError(t, err, "Task should be created without errors")
	event, err := eventRepo.GetEvent(common.CreateExtCtxWithArgs(context.Background(), nil), eventId)
	assert.NoError(t, err, "event should exist")

	_, err = eventBus.BeginTask(event.ID, events.SignalAll)
	assert.NoError(t, err, "Task should begin without errors")

	if !test.WaitForPredicateToBeTrue(func() bool {
		return coreStarted.Load() >= 1 &&
			broadcastStarted.Load() >= noBuses
	}) {
		t.Error("Expected started lifecycle handlers to receive task signal")
	}

	assert.Equal(t, int32(1), coreStarted.Load(), "core started handlers should run once")
	assert.Equal(t, noBuses, broadcastStarted.Load(), "broadcast started handlers should run on every bus")

	_, err = eventBus.CompleteTask(event.ID, &events.EventResult{}, events.EventStatusSuccess, events.SignalAll)
	assert.NoError(t, err, "Task should complete without errors")

	if !test.WaitForPredicateToBeTrue(func() bool {
		return coreCompleted.Load() >= 1 &&
			broadcastCompleted.Load() >= noBuses
	}) {
		t.Error("Expected completed lifecycle handlers to receive task signal")
	}

	assert.Equal(t, int32(1), coreCompleted.Load(), "core completed handlers should run once")
	assert.Equal(t, noBuses, broadcastCompleted.Load(), "broadcast completed handlers should run on every bus")

	_, err = eventRepo.GetEvent(common.CreateExtCtxWithArgs(context.Background(), nil), eventId)
	assert.NoError(t, err, "event should exist")
}

func requireReceivedEvent(t *testing.T, eventsReceived <-chan events.Event) events.Event {
	t.Helper()
	select {
	case event := <-eventsReceived:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("expected to receive an event")
		return events.Event{}
	}
}

func TestCreateTask(t *testing.T) {
	illID := apptest.GetIllTransId(t, illRepo)
	received := make(chan events.Event, 1)
	eventBus.HandleEventCreated(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
		if event.IllTransactionID == illID {
			received <- event
		}
	})

	_, err := eventBus.CreateTask(illID, events.EventNameRequestReceived, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
	require.NoError(t, err)
	assert.Equal(t, illID, requireReceivedEvent(t, received).IllTransactionID)
}

func TestTransactionRollback(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	eventId := uuid.New().String()
	illId := apptest.GetIllTransId(t, illRepo)
	err := eventRepo.WithTxFunc(appCtx, func(eventRepo events.EventRepo) error {
		_, err := eventRepo.SaveEvent(appCtx, events.SaveEventParams{
			ID:               eventId,
			IllTransactionID: illId,
			Timestamp:        apptest.GetNow(),
			EventType:        events.EventTypeTask,
			EventName:        events.EventNameMessageRequester,
			EventStatus:      events.EventStatusNew,
			EventData:        events.EventData{},
			PatronRequestID:  events.DEFAULT_PATRON_REQUEST_ID,
		})
		if err != nil {
			t.Error("Should not be error")
		}
		_, err = eventRepo.SaveEvent(appCtx, events.SaveEventParams{
			ID:               uuid.New().String(),
			IllTransactionID: uuid.New().String(),
			Timestamp:        apptest.GetNow(),
			EventType:        events.EventTypeTask,
			EventName:        events.EventNameMessageRequester,
			EventStatus:      events.EventStatusNew,
			EventData:        events.EventData{},
		})
		return err
	})
	if err == nil {
		t.Error("should be sql error")
	}
	_, err = eventRepo.GetEvent(appCtx, eventId)
	if err == nil {
		t.Error("should not find event")
	}
}

func TestGetBatchActionEvents(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	taskID := uuid.NewString()
	otherTaskID := uuid.NewString()
	patronRequestID := uuid.NewString()
	otherPatronRequestID := uuid.NewString()
	createPatronRequestForEventTest(t, patronRequestID)
	createPatronRequestForEventTest(t, otherPatronRequestID)
	now := time.Now()
	type eventFixture struct {
		taskID          string
		eventName       events.EventName
		patronRequestID string
	}
	fixtures := []eventFixture{
		{taskID, events.EventNameInvokeBatchAction, events.DEFAULT_PATRON_REQUEST_ID},
		{otherTaskID, events.EventNameInvokeBatchAction, events.DEFAULT_PATRON_REQUEST_ID},
		{taskID, events.EventNameInvokeBatchAction, events.DEFAULT_PATRON_REQUEST_ID},
		{taskID, events.EventNameInvokeBackgroundAction, patronRequestID},
		{taskID, events.EventNameInvokeAction, otherPatronRequestID},
	}
	for i, fixture := range fixtures {
		_, err := eventRepo.SaveEvent(ctx, events.SaveEventParams{
			ID: uuid.NewString(), IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID,
			PatronRequestID: fixture.patronRequestID,
			Timestamp:       pgtype.Timestamp{Time: now.Add(time.Duration(i) * time.Second), Valid: true},
			EventType:       events.EventTypeTask, EventName: fixture.eventName,
			EventStatus: events.EventStatusSuccess,
			EventData: events.EventData{CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{TaskId: fixture.taskID},
			}},
		})
		assert.NoError(t, err)
	}

	eventList, err := eventRepo.GetBatchActionEvents(ctx, taskID)
	assert.NoError(t, err)
	if assert.Len(t, eventList, 3) {
		assert.True(t, eventList[0].Timestamp.Time.After(eventList[1].Timestamp.Time))
		for _, event := range eventList {
			assert.Equal(t, taskID, event.EventData.BatchActionData.TaskId)
		}
		assert.Equal(t, events.EventNameInvokeBackgroundAction, eventList[0].EventName)
		assert.Equal(t, patronRequestID, eventList[0].PatronRequestID)
	}
}

func TestCreateNotice(t *testing.T) {
	illID := apptest.GetIllTransId(t, illRepo)
	received := make(chan events.Event, 1)
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
		if event.IllTransactionID == illID {
			received <- event
		}
	})

	_, err := eventBus.CreateNotice(illID, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess, events.EventDomainIllTransaction, events.SignalConsumers)
	require.NoError(t, err)
	event := requireReceivedEvent(t, received)
	assert.Equal(t, illID, event.IllTransactionID)
	assert.Equal(t, events.EventStatusSuccess, event.EventStatus)
}

func TestBatchNoticeWithMissingOrExpiredLeaseDoesNotBlockArchiving(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	pool := illRepo.(*ill_db.PgIllRepo).Pool
	for _, expired := range []bool{false, true} {
		name := "missing"
		if expired {
			name = "expired"
		}
		t.Run(name, func(t *testing.T) {
			workerID := uuid.NewString()
			now := time.Now().UTC()
			if expired {
				_, err := eventRepo.SaveEvent(ctx, events.SaveEventParams{
					ID: workerID, IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID, PatronRequestID: events.DEFAULT_PATRON_REQUEST_ID,
					EventType: events.EventTypeTask, EventName: events.EventNameInvokeBatchAction, EventStatus: events.EventStatusProcessing,
					Timestamp: pgtype.Timestamp{Time: now, Valid: true},
				})
				require.NoError(t, err)
				t.Cleanup(func() {
					_, err := pool.Exec(ctx, "DELETE FROM event WHERE id = $1", workerID)
					require.NoError(t, err)
				})
				_, err = pool.Exec(ctx, `WITH run AS (
					INSERT INTO batch_action_run (id, scheduled_task_id, owner, action_name)
					VALUES ($1, $1, '', 'overdue') RETURNING id
				)
				INSERT INTO batch_action_run_event (event_id, run_id, worker_event_id, lease_expires_at)
				SELECT id, id, id, clock_timestamp() - interval '1 second' FROM run`, workerID)
				require.NoError(t, err)
			}

			illID, owner := uuid.NewString(), "ISIL:ARCHIVE-"+uuid.NewString()
			_, err := illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{
				ID: illID, RequesterSymbol: pgtype.Text{String: owner, Valid: true},
				Timestamp: pgtype.Timestamp{Time: now.Add(-time.Hour), Valid: true},
			})
			require.NoError(t, err)
			candidate := ill_db.IllTransactionArchiveCandidate{ID: illID}
			require.NoError(t, pool.QueryRow(ctx, "SELECT xmin::text FROM ill_transaction WHERE id = $1", illID).Scan(&candidate.Version))

			noticeID := uuid.NewString()
			_, err = eventRepo.SaveEvent(ctx, events.SaveEventParams{
				ID: noticeID, IllTransactionID: illID, PatronRequestID: events.DEFAULT_PATRON_REQUEST_ID,
				EventType: events.EventTypeNotice, EventName: events.EventNameRequesterMsgReceived, EventStatus: events.EventStatusSuccess,
				Timestamp: pgtype.Timestamp{Time: now, Valid: true}, LastSignal: string(events.SignalNoticeCreated),
				EventData: events.EventData{BatchWorkerID: workerID},
			})
			require.NoError(t, err)
			var handled atomic.Bool
			eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
				if event.ID == noticeID {
					handled.Store(true)
				}
			})

			archived, err := illRepo.(*ill_db.PgIllRepo).ArchiveIllTransaction(ctx, candidate, owner, now, false)
			require.NoError(t, err)
			require.False(t, archived, "an unclaimed consumer notice blocks archiving")

			require.NoError(t, eventRepo.Notify(ctx, noticeID, events.SignalNoticeCreated, events.SignalConsumers))
			require.Eventually(t, func() bool {
				notice, err := eventRepo.GetEvent(ctx, noticeID)
				return err == nil && notice.LastSignal == "" && notice.EventStatus == events.EventStatusSuccess
			}, 5*time.Second, 10*time.Millisecond)
			require.Never(t, handled.Load, 100*time.Millisecond, 10*time.Millisecond, "the obsolete notice must not run its consumer")

			archived, err = illRepo.(*ill_db.PgIllRepo).ArchiveIllTransaction(ctx, candidate, owner, now, false)
			require.NoError(t, err)
			require.True(t, archived)
		})
	}
}

func TestBeginAndCompleteTask(t *testing.T) {
	illID := apptest.GetIllTransId(t, illRepo)
	received := make(chan events.Event, 1)
	started := make(chan events.Event, 1)
	completed := make(chan events.Event, 1)
	capture := func(sink chan<- events.Event) func(common.ExtendedContext, events.Event) {
		return func(_ common.ExtendedContext, event events.Event) {
			if event.IllTransactionID == illID {
				sink <- event
			}
		}
	}
	eventBus.HandleEventCreated(events.EventNameRequestReceived, events.HandlerRoleConsumer, capture(received))
	eventBus.HandleTaskStarted(events.EventNameRequestReceived, events.HandlerRoleConsumer, capture(started))
	eventBus.HandleTaskCompleted(events.EventNameRequestReceived, events.HandlerRoleConsumer, capture(completed))

	eventID, err := eventBus.CreateTask(illID, events.EventNameRequestReceived, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
	require.NoError(t, err)
	event := requireReceivedEvent(t, received)
	assert.Equal(t, illID, event.IllTransactionID)
	assert.Equal(t, eventID, event.ID)

	event, err = eventRepo.GetEvent(common.CreateExtCtxWithArgs(context.Background(), nil), eventID)
	require.NoError(t, err)
	assert.Equal(t, events.EventTypeTask, event.EventType)
	assert.Equal(t, events.EventStatusNew, event.EventStatus)

	_, err = eventBus.BeginTask(eventID, events.SignalConsumers)
	require.NoError(t, err)
	event = requireReceivedEvent(t, started)
	assert.Equal(t, eventID, event.ID)
	assert.Equal(t, events.EventStatusProcessing, event.EventStatus)

	_, err = eventBus.CompleteTask(eventID, &events.EventResult{}, events.EventStatusSuccess, events.SignalConsumers)
	require.NoError(t, err)
	event = requireReceivedEvent(t, completed)
	assert.Equal(t, eventID, event.ID)
	assert.Equal(t, events.EventStatusSuccess, event.EventStatus)
}

func TestProcessExclusiveTaskFailsWhenOlderIncompleteTaskExists(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	prID := uuid.NewString()
	createPatronRequestForEventTest(t, prID)
	_, err := eventBus.CreateTask(prID, events.EventNameInvokeAction, events.EventData{}, events.EventDomainPatronRequest, nil, events.SignalConsumers)
	assert.NoError(t, err)
	currentID, err := eventBus.CreateTask(prID, events.EventNameInvokeAction, events.EventData{}, events.EventDomainPatronRequest, nil, events.SignalConsumers)
	assert.NoError(t, err)

	handlerCalled := false
	completed, err := eventBus.ProcessExclusiveTask(appCtx, events.Event{ID: currentID}, events.SignalConsumers, func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
		handlerCalled = true
		return events.EventStatusSuccess, &events.EventResult{}
	})

	assert.NoError(t, err)
	assert.False(t, handlerCalled)
	assert.Equal(t, events.EventStatusError, completed.EventStatus)
	if assert.NotNil(t, completed.ResultData.EventError) {
		assert.Equal(t, "another invoke-action task in progress", completed.ResultData.EventError.Message)
	}
}

func TestProcessExclusiveTaskAllowsOlderAncestorTask(t *testing.T) {
	appCtx := common.CreateExtCtxWithArgs(context.Background(), nil)
	prID := uuid.NewString()
	createPatronRequestForEventTest(t, prID)
	parentID, err := eventBus.CreateTask(prID, events.EventNameInvokeAction, events.EventData{}, events.EventDomainPatronRequest, nil, events.SignalConsumers)
	assert.NoError(t, err)
	_, err = eventBus.BeginTask(parentID, events.SignalConsumers)
	assert.NoError(t, err)
	childID, err := eventBus.CreateTask(prID, events.EventNameInvokeAction, events.EventData{}, events.EventDomainPatronRequest, &parentID, events.SignalConsumers)
	assert.NoError(t, err)

	handlerCalled := false
	completed, err := eventBus.ProcessExclusiveTask(appCtx, events.Event{ID: childID}, events.SignalConsumers, func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult) {
		handlerCalled = true
		return events.EventStatusSuccess, &events.EventResult{}
	})
	assert.NoError(t, err)
	assert.True(t, handlerCalled)
	assert.Equal(t, events.EventStatusSuccess, completed.EventStatus)

	_, err = eventBus.CompleteTask(parentID, &events.EventResult{}, events.EventStatusSuccess, events.SignalConsumers)
	assert.NoError(t, err)
}

func createPatronRequestForEventTest(t *testing.T, prID string) {
	t.Helper()
	conn, err := pgx.Connect(context.Background(), app.ConnectionString)
	assert.NoError(t, err)
	defer conn.Close(context.Background())
	_, err = conn.Exec(context.Background(), "INSERT INTO patron_request (id, state, side) VALUES ($1, 'NEW', 'borrowing')", prID)
	assert.NoError(t, err)
}

func TestBeginTaskNegative(t *testing.T) {
	illId := apptest.GetIllTransId(t, illRepo)
	eventId := uuid.New().String()

	_, err := eventBus.BeginTask(eventId, events.SignalConsumers)
	if err == nil || err.Error() != "no rows in result set" {
		t.Errorf("Should fail with: no rows in result set")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	_, err = eventBus.BeginTask(eventId, events.SignalConsumers)
	if err == nil || err.Error() != "cannot begin task processing, event is not a TASK but NOTICE" {
		t.Errorf("Should fail with: cannot begin task processing, event is not a TASK but NOTICE")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	_, err = eventBus.BeginTask(eventId, events.SignalConsumers)
	if err == nil || err.Error() != "cannot begin task processing, event is not in state NEW but SUCCESS" {
		t.Errorf("Should fail with: event is not in state NEW but SUCCESS")
	}
}

func TestCompleteTaskNegative(t *testing.T) {
	illId := apptest.GetIllTransId(t, illRepo)
	eventId := uuid.New().String()

	result := events.EventResult{}
	_, err := eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess, events.SignalConsumers)
	if err == nil || err.Error() != "no rows in result set" {
		t.Errorf("Should fail with: no rows in result set")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	_, err = eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess, events.SignalConsumers)
	if err == nil || err.Error() != "cannot complete task processing, event is not a TASK but NOTICE" {
		t.Errorf("Should fail with: cannot complete task processing, event is not a TASK but NOTICE")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	_, err = eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess, events.SignalConsumers)
	if err == nil || err.Error() != "cannot complete task processing, event is not in state PROCESSING but SUCCESS" {
		t.Errorf("Should fail with: event is not in state PROCESSING but SUCCESS")
	}
}

func TestFailedToConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	freePort := testutil.GetFreePort(t)
	eventBus := events.NewPostgresEventBus(nil, fmt.Sprintf("postgres://crosslink:crosslink@localhost:%d/crosslink?sslmode=disable", freePort))
	err := eventBus.Start(common.CreateExtCtxWithArgs(ctx, nil))
	assert.Error(t, err, "Expected error when failing to connect to database")
	assert.Contains(t, err.Error(), "failed to connect to")
}

func TestReconnectListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, app.ConnectionString)
	require.NoError(t, err)
	defer conn.Close(context.Background())

	const listener = "FROM pg_stat_activity WHERE state = 'idle' AND query = 'LISTEN crosslink_channel'"
	_, err = conn.Exec(ctx, "SELECT pg_terminate_backend(pid) "+listener)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		var active bool
		err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 "+listener+")").Scan(&active)
		return err == nil && active
	}, 4*time.Second, 20*time.Millisecond)

	illID := apptest.GetIllTransId(t, illRepo)
	received := make(chan events.Event, 1)
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, events.HandlerRoleConsumer, func(_ common.ExtendedContext, event events.Event) {
		if event.IllTransactionID == illID {
			received <- event
		}
	})
	_, err = eventBus.CreateNotice(illID, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess, events.EventDomainIllTransaction, events.SignalConsumers)
	require.NoError(t, err)
	assert.Equal(t, illID, requireReceivedEvent(t, received).IllTransactionID)
}

func TestCreateTaskPreservesPostgresInsertErrorAndRollsBack(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	for _, parent := range []*string{nil, new("parent-event")} {
		id, err := eventBus.CreateTask(uuid.NewString(), events.EventNameInvokeBackgroundAction,
			events.EventData{}, events.EventDomainPatronRequest, parent, events.SignalConsumers)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr)
		assert.Equal(t, "23503", pgErr.Code, "return the foreign-key failure, not 25P02")
		_, err = eventRepo.GetEvent(ctx, id)
		require.ErrorIs(t, err, pgx.ErrNoRows, "the failed task must not be persisted")
	}
}
