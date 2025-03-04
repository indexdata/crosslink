package client

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/indexdata/go-utils/utils"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/client"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var LocalAddress = ""
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
	LocalAddress = "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo = test.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestMessageRequester(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := test.CreatePeer(t, illRepo, "isil:req", LocalAddress)
	illId := createIllTrans(t, illRepo, req.ID, string(iso18626.TypeActionReceived))
	resp := test.CreatePeer(t, illRepo, "isil:resp", LocalAddress)
	test.CreateLocatedSupplier(t, illRepo, illId, resp.ID, string(iso18626.TypeStatusLoaned))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
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
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := test.CreatePeer(t, illRepo, "isil:req", LocalAddress)
	illId := createIllTrans(t, illRepo, req.ID, string(iso18626.TypeActionReceived))
	resp := test.CreatePeer(t, illRepo, "isil:resp", LocalAddress)
	test.CreateLocatedSupplier(t, illRepo, illId, resp.ID, string(iso18626.TypeStatusLoaned))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			_, ok := event.ResultData.Data["response"]
			return ok
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
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := test.CreatePeer(t, illRepo, "isil:req", "invalid")
	illId := createIllTrans(t, illRepo, req.ID, string(iso18626.TypeActionReceived))
	resp := test.CreatePeer(t, illRepo, "isil:resp", "invalid")
	test.CreateLocatedSupplier(t, illRepo, illId, resp.ID, string(iso18626.TypeStatusLoaned))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
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
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := test.CreatePeer(t, illRepo, "isil:req", "invalid")
	illId := createIllTrans(t, illRepo, req.ID, string(iso18626.TypeActionReceived))
	resp := test.CreatePeer(t, illRepo, "isil:resp", "invalid")
	test.CreateLocatedSupplier(t, illRepo, illId, resp.ID, string(iso18626.TypeStatusLoaned))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
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
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := test.CreatePeer(t, illRepo, "isil:req", LocalAddress)
	illId := createIllTrans(t, illRepo, req.ID, string(iso18626.TypeActionReceived))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
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
	var receivedTasks []events.Event
	eventBus.HandleEventCreated(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		receivedTasks = append(receivedTasks, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	illId := createIllTrans(t, illRepo, "", string(iso18626.TypeActionReceived))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusProblem, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
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
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	illId := createIllTrans(t, illRepo, "", string(iso18626.TypeActionReceived))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusError
		}
		return false
	}) {
		t.Error("Expected to have error to send message")
	}
}

func TestMessageRequesterInvalidStatus(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := test.CreatePeer(t, illRepo, "isil:req", LocalAddress)
	illId := createIllTrans(t, illRepo, req.ID, string(iso18626.TypeActionReceived))
	resp := test.CreatePeer(t, illRepo, "isil:resp", LocalAddress)
	test.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "invalid")
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusError
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
}

func TestMessageSupplierInvalidAction(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := test.CreatePeer(t, illRepo, "isil:req", LocalAddress)
	illId := createIllTrans(t, illRepo, req.ID, "invalid")
	resp := test.CreatePeer(t, illRepo, "isil:resp", LocalAddress)
	test.CreateLocatedSupplier(t, illRepo, illId, resp.ID, string(iso18626.TypeStatusLoaned))
	eventId := test.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusProblem
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
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
			url:    "https://test.com",
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
			url:  "https://test.com",
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
			url:  "https://test.com",
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
			name:           "Marshal error",
			url:            "https://test.com/",
			msg:            nil,
			tenant:         "testTenant",
			mockResponse:   nil,
			mockError:      nil,
			expectedResult: nil,
			expectedError:  "marshal returned nil",
		},
		{
			name: "Invalid address",
			url:  "https://test.com/\x7f",
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

func createIllTrans(t *testing.T, illRepo ill_db.IllRepo, requester string, action string) string {
	var requesterId pgtype.Text
	if requester != "" {
		requesterId = pgtype.Text{
			String: requester,
			Valid:  true,
		}
	}
	illId := uuid.New().String()
	_, err := illRepo.SaveIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID:          illId,
		Timestamp:   test.GetNow(),
		RequesterID: requesterId,
		LastRequesterAction: pgtype.Text{
			String: action,
			Valid:  true,
		},
	})
	if err != nil {
		t.Errorf("Failed to create ill transaction: %s", err)
	}
	return illId
}
