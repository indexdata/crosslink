package events

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/indexdata/go-utils/utils"
	"github.com/stretchr/testify/assert"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5"
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

	pgContainer, err := postgres.Run(ctx, "postgres",
		postgres.WithDatabase("crosslink"),
		postgres.WithUsername("crosslink"),
		postgres.WithPassword("crosslink"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(5*time.Second)),
	)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	app.HTTP_PORT = utils.Must(test.GetFreePort())

	ctx, cancel := context.WithCancel(context.Background())
	eventBus, illRepo, eventRepo = apptest.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	defer cancel()
	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestMultipleEventHandlers(t *testing.T) {
	var eventBus2 events.EventBus
	var illRepo2 ill_db.IllRepo
	var eventRepo2 events.EventRepo
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	ctx2, cancel2 := context.WithCancel(context.Background())
	eventBus2, illRepo2, eventRepo2 = apptest.StartApp(ctx2)

	assert.NotNil(t, illRepo2, "Event bus should not be nil")
	assert.NotNil(t, eventBus2, "Event bus should not be nil")
	assert.NotNil(t, eventRepo2, "Event repo should not be nil")
	defer cancel2()

	var requestReceived2 []events.Event
	eventBus2.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		requestReceived2 = append(requestReceived2, event)
	})

	var requestReceived1 []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		requestReceived1 = append(requestReceived1, event)
	})

	illId := apptest.GetIllTransId(t, illRepo2)
	_, err := eventBus2.CreateTask(illId, events.EventNameRequestReceived, events.EventData{}, nil)
	assert.NoError(t, err, "Task should be created without errors")

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(requestReceived2) == 1 && len(requestReceived1) == 1
	}) {
		t.Error("Expected to have both request event received")
	}
}

func TestCreateTask(t *testing.T) {
	var requestReceived []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		requestReceived = append(requestReceived, event)
	})
	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{}, nil)
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
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
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

func TestCreateNotice(t *testing.T) {
	var eventReceived []events.Event
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventReceived = append(eventReceived, event)
	})

	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateNotice(illId, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess)
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
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventsReceived = append(eventsReceived, event)
	})
	eventBus.HandleTaskStarted(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventsStarted = append(eventsStarted, event)
	})
	eventBus.HandleTaskCompleted(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventsCompleted = append(eventsCompleted, event)
	})

	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{}, nil)
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

	err = eventBus.BeginTask(eventId)
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
	err = eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess)
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

func TestBeginTaskNegative(t *testing.T) {
	illId := apptest.GetIllTransId(t, illRepo)
	eventId := uuid.New().String()

	err := eventBus.BeginTask(eventId)
	if err == nil || err.Error() != "no rows in result set" {
		t.Errorf("Should fail with: no rows in result set")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	err = eventBus.BeginTask(eventId)
	if err == nil || err.Error() != "event is not a TASK" {
		t.Errorf("Should fail with: event is not a TASK")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	err = eventBus.BeginTask(eventId)
	if err == nil || err.Error() != "event is not in state NEW" {
		t.Errorf("Should fail with: event is not in state NEW")
	}
}

func TestCompleteTaskNegative(t *testing.T) {
	illId := apptest.GetIllTransId(t, illRepo)
	eventId := uuid.New().String()

	result := events.EventResult{}
	err := eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess)
	if err == nil || err.Error() != "no rows in result set" {
		t.Errorf("Should fail with: no rows in result set")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	err = eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess)
	if err == nil || err.Error() != "event is not a TASK" {
		t.Errorf("Should fail with: event is not a TASK")
	}

	eventId = apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusSuccess, events.EventNameRequesterMsgReceived)

	err = eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess)
	if err == nil || err.Error() != "event is not in state PROCESSING" {
		t.Errorf("Should fail with: event is not in state PROCESSING")
	}
}

func TestFailedToConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus := events.NewPostgresEventBus(nil, "postgres://crosslink:crosslink@localhost:111/crosslink?sslmode=disable")
	err := eventBus.Start(extctx.CreateExtCtxWithArgs(ctx, nil))
	if err == nil || strings.Index(err.Error(), "failed to connect to") > 0 {
		t.Errorf("Should fail with: ailed to connect to ... but had %s", err.Error())
	}
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
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventReceived = append(eventReceived, event)
	})

	illId := apptest.GetIllTransId(t, illRepo)

	_, err := eventBus.CreateNotice(illId, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess)
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(eventReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}
}
