package events

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

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
	if err != nil {
		panic(fmt.Sprintf("failed to start db container: %s", err))
	}

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic(fmt.Sprintf("failed to get conn string: %s", err))
	}

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	app.HTTP_PORT = 19082

	time.Sleep(1 * time.Second)

	code := m.Run()

	if err := pgContainer.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("failed to stop db container: %s", err))
	}
	os.Exit(code)
}
func TestEventHandling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, _, _ := startApp(ctx)
	var requestReceived = []events.Event{}
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		requestReceived = append(requestReceived, event)
	})

	data, _ := os.ReadFile("../testdata/request.xml")
	req, _ := http.NewRequest("POST", "http://localhost:19082/iso18626", bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	resp, err := client.Do(req)

	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status %d, got %d", 200, resp.StatusCode)
	}

	if !waitForPredicateToBeTrue(func() bool {
		return len(requestReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}
}

func TestCreateTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, _ := startApp(ctx)
	var requestReceived = []events.Event{}
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		requestReceived = append(requestReceived, event)
	})
	illId := createIllTrans(t, illRepo)

	err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{})
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !waitForPredicateToBeTrue(func() bool {
		return len(requestReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}

	if requestReceived[0].IllTransactionID != illId {
		t.Errorf("Ill transaction id does not match, expected %s, got %s", illId, requestReceived[0].IllTransactionID)
	}
}

func TestCreateNotice(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, _ := startApp(ctx)
	var eventReceived = []events.Event{}
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventReceived = append(eventReceived, event)
	})

	illId := createIllTrans(t, illRepo)

	err := eventBus.CreateNotice(illId, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess)
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !waitForPredicateToBeTrue(func() bool {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, _ := startApp(ctx)
	var eventsReceived = []events.Event{}
	var eventsStarted = []events.Event{}
	var eventsCompleted = []events.Event{}
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventsReceived = append(eventsReceived, event)
	})
	eventBus.HandleTaskStarted(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventsStarted = append(eventsStarted, event)
	})
	eventBus.HandleTaskCompleted(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventsCompleted = append(eventsCompleted, event)
	})

	illId := createIllTrans(t, illRepo)

	err := eventBus.CreateTask(illId, events.EventNameRequestReceived, events.EventData{})
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !waitForPredicateToBeTrue(func() bool {
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
	if !waitForPredicateToBeTrue(func() bool {
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
	if !waitForPredicateToBeTrue(func() bool {
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
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventBus, illRepo, eventRepo := startApp(ctx)

	illId := createIllTrans(t, illRepo)
	eventId := uuid.New().String()

	err := eventBus.BeginTask(eventId)
	if err == nil || err.Error() != "no rows in result set" {
		t.Errorf("Should fail with: no rows in result set")
	}

	eventId = createEvent(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess)

	err = eventBus.BeginTask(eventId)
	if err == nil || err.Error() != "event is not a TASK" {
		t.Errorf("Should fail with: event is not a TASK")
	}

	eventId = createEvent(t, eventRepo, illId, events.EventTypeTask, events.EventStatusSuccess)

	err = eventBus.BeginTask(eventId)
	if err == nil || err.Error() != "event is not in state NEW" {
		t.Errorf("Should fail with: event is not in state NEW")
	}
}

func TestCompleteTaskNegative(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eventBus, illRepo, eventRepo := startApp(ctx)

	illId := createIllTrans(t, illRepo)
	eventId := uuid.New().String()

	result := events.EventResult{}
	err := eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess)
	if err == nil || err.Error() != "no rows in result set" {
		t.Errorf("Should fail with: no rows in result set")
	}

	eventId = createEvent(t, eventRepo, illId, events.EventTypeNotice, events.EventStatusSuccess)

	err = eventBus.CompleteTask(eventId, &result, events.EventStatusSuccess)
	if err == nil || err.Error() != "event is not a TASK" {
		t.Errorf("Should fail with: event is not a TASK")
	}

	eventId = createEvent(t, eventRepo, illId, events.EventTypeTask, events.EventStatusSuccess)

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

func TestReconnectListenner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, _ := startApp(ctx)

	// Force App to reconnect to postgres LISTENER
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := pgx.Connect(ctx, app.ConnectionString)
		if err != nil {
			t.Errorf("reconnect test unable to connect to database: %s", err)
		}

		_, err = conn.Exec(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE state = 'idle' AND query LIKE 'LISTEN%'")
		if err != nil {
			t.Errorf("reconnect test unable to kill listen command: %s", err)
		}
	}()
	wg.Wait()
	// Wait for reconnect
	time.Sleep(1000 * time.Millisecond)

	var eventReceived = []events.Event{}
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		eventReceived = append(eventReceived, event)
	})

	illId := createIllTrans(t, illRepo)

	err := eventBus.CreateNotice(illId, events.EventNameSupplierMsgReceived, events.EventData{}, events.EventStatusSuccess)
	if err != nil {
		t.Errorf("Task should be created without errors: %s", err)
	}

	if !waitForPredicateToBeTrue(func() bool {
		return len(eventReceived) == 1
	}) {
		t.Error("Expected to have request event received")
	}
}

func startApp(ctx context.Context) (events.EventBus, ill_db.IllRepo, events.EventRepo) {
	var eventBus events.EventBus
	var illRepo ill_db.IllRepo
	var eventRepo events.EventRepo
	go func() {
		app.RunMigrateScripts()
		pool := app.InitDbPool()
		eventRepo = app.CreateEventRepo(pool)
		eventBus = app.CreateEventBus(eventRepo)
		app.StartEventBus(ctx, eventBus)
		illRepo = app.CreateIllRepo(pool)
		app.StartApp(illRepo, eventBus)
	}()
	time.Sleep(100 * time.Millisecond)
	return eventBus, illRepo, eventRepo
}

func waitForPredicateToBeTrue(predicate func() bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond) // Check every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
			if predicate() {
				return true
			}
		}
	}
}

func createIllTrans(t *testing.T, illRepo ill_db.IllRepo) string {
	illId := uuid.New().String()
	_, err := illRepo.CreateIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.CreateIllTransactionParams{
		ID:        illId,
		Timestamp: getNow(),
	})
	if err != nil {
		t.Errorf("Failed to create ill transaction: %s", err)
	}
	return illId
}

func createEvent(t *testing.T, eventRepo events.EventRepo, illId string, eventType events.EventType, status events.EventStatus) string {
	eventId := uuid.New().String()
	_, err := eventRepo.SaveEvent(extctx.CreateExtCtxWithArgs(context.Background(), nil), events.SaveEventParams{
		ID:               eventId,
		IllTransactionID: illId,
		Timestamp:        getNow(),
		EventType:        eventType,
		EventName:        events.EventNameRequesterMsgReceived,
		EventStatus:      status,
		EventData:        events.EventData{},
	})

	if err != nil {
		t.Errorf("Failed to create event: %s", err)
	}
	return eventId
}

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
