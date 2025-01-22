package client

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/client"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var LOCAL_ADDRESS = "http://localhost:19082/iso18626"

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

func TestMessageRequester(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ := startApp(ctx)
	var completedTask = []events.Event{}
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := createPeer(t, appCtx, illRepo, "isil:req", LOCAL_ADDRESS)
	illId := createIllTrans(t, appCtx, illRepo, req.ID)
	resp := createPeer(t, appCtx, illRepo, "isil:resp", LOCAL_ADDRESS)
	createLocatedSupplier(t, appCtx, illRepo, illId, resp.ID)
	eventId := createEvent(t, appCtx, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !waitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if _, ok := event.ResultData.Data["response"]; !ok {
		t.Error("Should have response in result data")
	}
}

func TestMessageSupplier(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ := startApp(ctx)
	var completedTask = []events.Event{}
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := createPeer(t, appCtx, illRepo, "isil:req", LOCAL_ADDRESS)
	illId := createIllTrans(t, appCtx, illRepo, req.ID)
	resp := createPeer(t, appCtx, illRepo, "isil:resp", LOCAL_ADDRESS)
	createLocatedSupplier(t, appCtx, illRepo, illId, resp.ID)
	eventId := createEvent(t, appCtx, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !waitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if _, ok := event.ResultData.Data["response"]; !ok {
		t.Error("Should have response in result data")
	}
}

func TestMessageRequesterInvalidAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ := startApp(ctx)
	var completedTask = []events.Event{}
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := createPeer(t, appCtx, illRepo, "isil:req", "invalid")
	illId := createIllTrans(t, appCtx, illRepo, req.ID)
	resp := createPeer(t, appCtx, illRepo, "isil:resp", "invalid")
	createLocatedSupplier(t, appCtx, illRepo, illId, resp.ID)
	eventId := createEvent(t, appCtx, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !waitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusError
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if _, ok := event.ResultData.Data["error"]; !ok {
		t.Error("Should have error in result data")
	}
}

func TestMessageSupplierInvalidAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ := startApp(ctx)
	var completedTask = []events.Event{}
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := createPeer(t, appCtx, illRepo, "isil:req", "invalid")
	illId := createIllTrans(t, appCtx, illRepo, req.ID)
	resp := createPeer(t, appCtx, illRepo, "isil:resp", "invalid")
	createLocatedSupplier(t, appCtx, illRepo, illId, resp.ID)
	eventId := createEvent(t, appCtx, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !waitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusError
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if _, ok := event.ResultData.Data["error"]; !ok {
		t.Error("Should have error in result data")
	}
}

func TestMessageSupplierMissingSupplier(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ := startApp(ctx)
	var completedTask = []events.Event{}
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := createPeer(t, appCtx, illRepo, "isil:req", LOCAL_ADDRESS)
	illId := createIllTrans(t, appCtx, illRepo, req.ID)
	eventId := createEvent(t, appCtx, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !waitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusError
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if _, ok := event.ResultData.Data["error"]; !ok {
		t.Error("Should have error in result data")
	}
}

func TestMessageRequesterFailToBegin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ := startApp(ctx)
	var receivedTasks = []events.Event{}
	eventBus.HandleEventCreated(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		receivedTasks = append(receivedTasks, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	illId := createIllTrans(t, appCtx, illRepo, "")
	eventId := createEvent(t, appCtx, eventRepo, illId, events.EventTypeTask, events.EventStatusProblem, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !waitForPredicateToBeTrue(func() bool {
		if len(receivedTasks) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, receivedTasks[0].ID)
			return event.EventStatus == events.EventStatusProblem
		}
		return false
	}) {
		t.Error("Expected to not change event status")
	}
}

func TestMessageRequesterCompleteWithError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ := startApp(ctx)
	var completedTask = []events.Event{}
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	illId := createIllTrans(t, appCtx, illRepo, "")
	eventId := createEvent(t, appCtx, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !waitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusError
		}
		return false
	}) {
		t.Error("Expected to have error to send message")
	}
}

type MockRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func TestSendHttpPost(t *testing.T) {
	// Define test cases
	tests := []struct {
		name           string
		url            string
		msg            *iso18626.ISO18626Message
		tenant         string
		mockResponse   *http.Response
		mockError      error
		expectedResult *iso18626.ISO18626Message
		expectedError  string
	}{
		{
			name:   "successful post request",
			url:    "http://test.com",
			msg:    &iso18626.ISO18626Message{},
			tenant: "testTenant",
			mockResponse: &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(bytes.NewBufferString(`<ISO18626Message>
					<!-- Add your mock XML response -->
				</ISO18626Message>`)),
			},
			mockError:      nil,
			expectedResult: &iso18626.ISO18626Message{},
			expectedError:  "",
		},
		{
			name: "server error",
			url:  "http://test.com",
			msg:  &iso18626.ISO18626Message{
				// Fill in the fields with test data
			},
			tenant: "testTenant",
			mockResponse: &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString("Internal Server Error")),
			},
			mockError:      nil,
			expectedResult: nil,
			expectedError:  "500: Internal Server Error",
		},
		{
			name: "request error",
			url:  "http://test.com",
			msg:  &iso18626.ISO18626Message{
				// Fill in the fields with test data
			},
			tenant:         "testTenant",
			mockResponse:   nil,
			mockError:      fmt.Errorf("mock request error"),
			expectedResult: nil,
			expectedError:  "mock request error",
		},
		{
			name: "Marshal error",
			url:  "http://test.com/\x7f",
			msg: &iso18626.ISO18626Message{
				Request: &iso18626.Request{
					Header: iso18626.Header{
						SupplyingAgencyRequestId: "test\x00\x1Fdata\"<>&",
					},
				},
			},
			tenant:         "testTenant",
			mockResponse:   nil,
			mockError:      nil,
			expectedResult: nil,
			expectedError:  "invalid control character in URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockTransport := &MockRoundTripper{
				roundTripFunc: func(req *http.Request) (*http.Response, error) {
					if tt.mockError != nil {
						return nil, tt.mockError
					}
					return tt.mockResponse, nil
				},
			}

			httpClient := &http.Client{Transport: mockTransport}
			isoClient := client.CreateIso18626ClientWithHttpClient(httpClient)

			result, err := isoClient.SendHttpPost(tt.url, tt.msg, tt.tenant)

			if tt.expectedError == "" && err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.expectedError != "" && (err == nil || !strings.Contains(err.Error(), tt.expectedError)) {
				t.Fatalf("expected error %q, got %v", tt.expectedError, err)
			}

			if tt.expectedResult != nil && result != nil {
				expectedXML, _ := xml.Marshal(tt.expectedResult)
				resultXML, _ := xml.Marshal(result)
				if !bytes.Equal(expectedXML, resultXML) {
					t.Errorf("expected result %s, got %s", expectedXML, resultXML)
				}
			} else if tt.expectedResult != nil || result != nil {
				t.Errorf("expected result %v, got %v", tt.expectedResult, result)
			}
		})
	}
}

func createIllTrans(t *testing.T, ctx extctx.ExtendedContext, illRepo ill_db.IllRepo, requester string) string {
	var requesterId pgtype.Text
	if requester != "" {
		requesterId = pgtype.Text{
			String: requester,
			Valid:  true,
		}
	}
	illId := uuid.New().String()
	_, err := illRepo.CreateIllTransaction(ctx, ill_db.CreateIllTransactionParams{
		ID:          illId,
		Timestamp:   getNow(),
		RequesterID: requesterId,
	})
	if err != nil {
		t.Errorf("Failed to create ill transaction: %s", err)
	}
	return illId
}

func createPeer(t *testing.T, ctx extctx.ExtendedContext, illRepo ill_db.IllRepo, symbol string, address string) ill_db.Peer {
	peer, err := illRepo.CreatePeer(ctx, ill_db.CreatePeerParams{
		ID:     uuid.New().String(),
		Symbol: symbol,
		Name:   symbol,
		Address: pgtype.Text{
			String: address,
			Valid:  true,
		},
	})
	if err != nil {
		t.Errorf("Failed to create peer: %s", err)
	}
	return peer
}

func createLocatedSupplier(t *testing.T, ctx extctx.ExtendedContext, illRepo ill_db.IllRepo, illTransId string, supplierId string) ill_db.LocatedSupplier {
	supplier, err := illRepo.CreateLocatedSupplier(ctx, ill_db.CreateLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: illTransId,
		SupplierID:       supplierId,
		Ordinal:          0,
		SupplierStatus: pgtype.Text{
			String: "selected",
			Valid:  true,
		},
	})
	if err != nil {
		t.Errorf("Failed to create peer: %s", err)
	}
	return supplier
}

func createEvent(t *testing.T, ctx extctx.ExtendedContext, eventRepo events.EventRepo, illId string, eventType events.EventType, status events.EventStatus, eventName events.EventName) string {
	eventId := uuid.New().String()
	_, err := eventRepo.SaveEvent(ctx, events.SaveEventParams{
		ID:               eventId,
		IllTransactionID: illId,
		Timestamp:        getNow(),
		EventType:        eventType,
		EventName:        eventName,
		EventStatus:      status,
		EventData:        events.EventData{},
	})

	if err != nil {
		t.Errorf("Failed to create event: %s", err)
	}
	return eventId
}

func startApp(ctx context.Context) (events.EventBus, ill_db.IllRepo, events.EventRepo, client.Iso18626Client) {
	var eventBus events.EventBus
	var illRepo ill_db.IllRepo
	var eventRepo events.EventRepo
	var iso18626Client client.Iso18626Client
	go func() {
		app.RunMigrateScripts()
		pool := app.InitDbPool()
		eventRepo = app.CreateEventRepo(pool)
		eventBus = app.CreateEventBus(eventRepo)
		illRepo = app.CreateIllRepo(pool)
		iso18626Client = client.CreateIso18626Client(eventBus, illRepo)
		app.AddDefaultHandlers(eventBus, iso18626Client)
		app.StartEventBus(ctx, eventBus)
		app.StartApp(illRepo, eventBus)
	}()
	time.Sleep(100 * time.Millisecond)
	return eventBus, illRepo, eventRepo, iso18626Client
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

func getNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}
