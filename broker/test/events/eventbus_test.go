package events

import (
	"bytes"
	"context"
	"fmt"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"net/http"
	"os"
	"testing"
	"time"
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
	var eventBus events.EventBus
	var requestReceived = []events.Event{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		app.RunMigrateScripts()
		pool := app.InitDbPool()
		eventRepo := app.CreateEventRepo(pool)
		eventBus = app.InitEventBus(ctx, eventRepo)
		eventBus.HandleEventCreated(events.EventNameRequestReceived, func(event events.Event) {
			requestReceived = append(requestReceived, event)
		})
		illRepo := app.CreateIllRepo(pool)
		app.StartApp(illRepo, eventBus)
	}()
	time.Sleep(100 * time.Millisecond)

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
