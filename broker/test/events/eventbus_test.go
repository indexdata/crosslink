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
	"github.com/indexdata/crosslink/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
)

var eventBus events.EventBus
var illRepo ill_db.IllRepo
var eventRepo events.EventRepo

func TestMain(m *testing.M) {
	ctx := context.Background()
	app.DB_PROVISION = true

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second)),
	)
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

func TestCreateTask(t *testing.T) {
	var requestReceived []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		requestReceived = append(requestReceived, event)
	})
	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(requestReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}

	if requestReceived[0].IllTransactionID != illId {
		t.Errorf("Ill transaction id does not match, expected %s, got %s", illId, requestReceived[0].IllTransactionID)
	}
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
	now := time.Now()
	for i, id := range []string{taskID, otherTaskID, taskID} {
		_, err := eventRepo.SaveEvent(ctx, events.SaveEventParams{
			ID: uuid.NewString(), IllTransactionID: events.DEFAULT_ILL_TRANSACTION_ID,
			PatronRequestID: events.DEFAULT_PATRON_REQUEST_ID,
			Timestamp:       pgtype.Timestamp{Time: now.Add(time.Duration(i) * time.Second), Valid: true},
			EventType:       events.EventTypeTask, EventName: events.EventNameInvokeBatchAction,
			EventStatus: events.EventStatusSuccess,
			EventData: events.EventData{CommonEventData: events.CommonEventData{
				BatchActionData: &events.BatchActionData{TaskId: id},
			}},
		})
		assert.NoError(t, err)
	}

	eventList, err := eventRepo.GetBatchActionEvents(ctx, taskID)
	assert.NoError(t, err)
	if assert.Len(t, eventList, 2) {
		assert.True(t, eventList[0].Timestamp.Time.After(eventList[1].Timestamp.Time))
		for _, event := range eventList {
			assert.Equal(t, taskID, event.EventData.BatchActionData.TaskId)
		}
	}
}

func TestCreateNotice(t *testing.T) {
	var eventReceived []events.Event
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		eventReceived = append(eventReceived, event)
	})

	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateNotice(illId, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess, events.EventDomainIllTransaction, events.SignalConsumers)
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(eventReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}

	if eventReceived[0].IllTransactionID != illId {
		t.Errorf("Ill transaction id does not match, expected %s, got %s", illId, eventReceived[0].IllTransactionID)
	}

	if eventReceived[0].EventStatus != events.EventStatusSuccess {
		t.Errorf("Event status does not match, expected %s, got %s", events.EventStatusSuccess, eventReceived[0].EventStatus)
	}
}

func TestBeginAndCompleteTask(t *testing.T) {
	var eventsReceived []events.Event
	var eventsStarted []events.Event
	var eventsCompleted []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		eventsReceived = append(eventsReceived, event)
	})
	eventBus.HandleTaskStarted(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		eventsStarted = append(eventsStarted, event)
	})
	eventBus.HandleTaskCompleted(events.EventNameRequestReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		eventsCompleted = append(eventsCompleted, event)
	})

	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{}, events.EventDomainIllTransaction, nil, events.SignalConsumers)
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(eventsReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}

	if eventsReceived[0].IllTransactionID != illId {
		t.Errorf("Ill transaction id does not match, expected %s, got %s", illId, eventsReceived[0].IllTransactionID)
	}

	eventId := eventsReceived[0].ID

	event, err := eventRepo.GetEvent(common.CreateExtCtxWithArgs(context.Background(), nil), eventId)
	assert.NoError(t, err, "Should not be error getting event")
	assert.Equal(t, events.EventTypeTask, event.EventType, "Event type should be TASK")
	assert.Equal(t, events.EventStatusNew, event.EventStatus, "Event status should be NEW")

	_, err = eventBus.BeginTask(eventId, events.SignalConsumers)
	if err != nil {
		t.Errorf("Task should be started: %s", err)
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(eventsStarted) == 1
	}) {
		t.Error("Expected to have request event received")
	}
	if eventsStarted[0].ID != eventId {
		t.Errorf("Event id does not match, expected %s, got %s", eventId, eventsStarted[0].ID)
	}
	if eventsStarted[0].EventStatus != events.EventStatusProcessing {
		t.Errorf("Event status does not match, expected %s, got %s", eventId, eventsStarted[0].EventStatus)
	}

	result := events.EventResult{}
	_, err = eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess, events.SignalConsumers)
	if err != nil {
		t.Errorf("Task should be started: %s", err)
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(eventsCompleted) == 1
	}) {
		t.Error("Expected to have request event received")
	}
	if eventsCompleted[0].ID != eventId {
		t.Errorf("Event id does not match, expected %s, got %s", eventId, eventsCompleted[0].ID)
	}
	if eventsCompleted[0].EventStatus != events.EventStatusSuccess {
		t.Errorf("Event status does not match, expected %s, got %s", events.EventStatusSuccess, eventsCompleted[0].EventStatus)
	}
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
	// Force App to reconnect to postgres LISTENER
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := pgx.Connect(context.Background(), app.ConnectionString)
		if err != nil {
			t.Errorf("reconnect test unable to connect to database: %s", err)
		}

		_, err = conn.Exec(context.Background(), "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE state = 'idle' AND query LIKE 'LISTEN%'")
		if err != nil {
			t.Errorf("reconnect test unable to kill listen command: %s", err)
		}
	}()
	wg.Wait()
	// Wait for reconnect
	time.Sleep(2000 * time.Millisecond)

	var eventReceived []events.Event
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, events.HandlerRoleConsumer, func(ctx common.ExtendedContext, event events.Event) {
		eventReceived = append(eventReceived, event)
	})

	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateNotice(illId, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess, events.EventDomainIllTransaction, events.SignalConsumers)
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(eventReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}
}
