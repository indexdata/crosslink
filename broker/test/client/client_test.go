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

	"github.com/indexdata/crosslink/broker/adapter"
	mockapp "github.com/indexdata/crosslink/illmock/app"
	"github.com/stretchr/testify/assert"

	"github.com/indexdata/go-utils/utils"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/client"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	test "github.com/indexdata/crosslink/broker/test/utils"
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
	app.BROKER_MODE = string(extctx.BrokerModeTransparent)
	mockPort := strconv.Itoa(utils.Must(test.GetFreePort()))
	LocalAddress = "http://localhost:" + strconv.Itoa(app.HTTP_PORT) + "/iso18626"
	test.Expect(os.Setenv("HTTP_PORT", mockPort), "failed to set mock client port")
	test.Expect(os.Setenv("PEER_URL", LocalAddress), "failed to set peer URL")

	adapter.MOCK_CLIENT_URL = "http://localhost:" + mockPort + "/iso18626"

	go func() {
		var mockApp mockapp.MockApp
		test.Expect(mockApp.Run(), "failed to start illmock client")
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo = apptest.StartApp(ctx)
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
	//we use the mock as the destination for the request to get a valid ISO18626 response
	reqPeer := apptest.CreatePeer(t, illRepo, "ISIL:REQ1", adapter.MOCK_CLIENT_URL)
	supPeer := apptest.CreatePeer(t, illRepo, "ISIL:RESP1", adapter.MOCK_CLIENT_URL)
	illId := createIllTrans(t, illRepo, reqPeer.ID, "ISIL:REQ1", string(iso18626.TypeActionReceived), "ISIL:RESP1")
	apptest.CreateLocatedSupplier(t, illRepo, illId, supPeer.ID, "ISIL:RESP1", string(iso18626.TypeStatusLoaned))
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusProblem //mock will report confirmation error bc of missing request
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if event.ResultData.IncomingMessage == nil {
		t.Error("Should have response in result data")
	}
	assert.Equal(t, "REQ1", event.ResultData.OutgoingMessage.SupplyingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "RESP1", event.ResultData.OutgoingMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
}

func TestMessageRequesterWithBrokerModePerPeer(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := apptest.CreatePeerWithMode(t, illRepo, "ISIL:REQ1", adapter.MOCK_CLIENT_URL, string(extctx.BrokerModeOpaque))
	resp := apptest.CreatePeerWithMode(t, illRepo, "ISIL:RESP1", adapter.MOCK_CLIENT_URL, string(extctx.BrokerModeOpaque))
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ1", string(iso18626.TypeActionReceived), "ISIL:RESP1")
	apptest.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "ISIL:RESP1", string(iso18626.TypeStatusLoaned))
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusProblem //mock will report confirmation error bc of missing request
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if event.ResultData.IncomingMessage == nil {
		t.Error("Should have response in result data")
	}
	if event.ResultData.OutgoingMessage == nil {
		t.Error("Should have request in result data")
	}
	assert.Equal(t, "REQ1", event.ResultData.OutgoingMessage.SupplyingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "BROKER", event.ResultData.OutgoingMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
}
func TestMessageRequesterNoLastStatus(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := apptest.CreatePeer(t, illRepo, "ISIL:REQ1_1", adapter.MOCK_CLIENT_URL)
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ1_1", string(iso18626.TypeActionReceived), "ISIL:RESP1_1")
	resp := apptest.CreatePeer(t, illRepo, "ISIL:RESP1_1", adapter.MOCK_CLIENT_URL)
	apptest.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "ISIL:RESP1_1", "") //no status
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusProblem //mock will report confirmation error bc of missing request
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	event, _ := eventRepo.GetEvent(appCtx, completedTask[0].ID)
	if event.ResultData.IncomingMessage == nil {
		t.Error("Should have response in result data")
	}
	assert.Equal(t, "REQ1_1", event.ResultData.OutgoingMessage.SupplyingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue)
	assert.Equal(t, "RESP1_1", event.ResultData.OutgoingMessage.SupplyingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
	assert.Equal(t, iso18626.TypeStatusExpectToSupply, event.ResultData.OutgoingMessage.SupplyingAgencyMessage.StatusInfo.Status)
}

func TestMessageSupplier(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := apptest.CreatePeer(t, illRepo, "ISIL:REQ2", adapter.MOCK_CLIENT_URL)
	resp := apptest.CreatePeer(t, illRepo, "ISIL:RESP2", adapter.MOCK_CLIENT_URL)
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ2", string(iso18626.TypeActionReceived), "ISIL:RESP2")
	apptest.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "ISIL:RESP2", string(iso18626.TypeStatusLoaned))
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}
	var event events.Event
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) >= 1 {
			event, _ = eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusSuccess //mock will report success even though the request does not exist
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	if event.ResultData.IncomingMessage == nil {
		t.Error("Should have response in result data")
	}
	assert.Equal(t, "RESP2", event.ResultData.OutgoingMessage.RequestingAgencyMessage.Header.SupplyingAgencyId.AgencyIdValue)
	assert.Equal(t, "REQ2", event.ResultData.OutgoingMessage.RequestingAgencyMessage.Header.RequestingAgencyId.AgencyIdValue)
}

func TestMessageRequesterInvalidAddress(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := apptest.CreatePeer(t, illRepo, "ISIL:REQ3", "invalid")
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ3", string(iso18626.TypeActionReceived), "ISIL:RESP3")
	resp := apptest.CreatePeer(t, illRepo, "ISIL:RESP3", "invalid")
	apptest.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "ISIL:RESP3", string(iso18626.TypeStatusLoaned))
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
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
	if event.ResultData.EventError == nil {
		t.Error("Should have error in result data")
	}
}

func TestMessageSupplierInvalidAddress(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := apptest.CreatePeer(t, illRepo, "ISIL:REQ4", "invalid")
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ4", string(iso18626.TypeActionReceived), "ISIL:RESP4")
	resp := apptest.CreatePeer(t, illRepo, "ISIL:RESP4", "invalid")
	apptest.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "ISIL:RESP4", string(iso18626.TypeStatusLoaned))
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
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
	if event.ResultData.EventError == nil {
		t.Error("Should have error in result data")
	}
}

func TestMessageSupplierMissingSupplier(t *testing.T) {
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	req := apptest.CreatePeer(t, illRepo, "ISIL:REQ7", "whatever")
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ7", string(iso18626.TypeActionReceived), "ISIL:RESP7")
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
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
	if event.ResultData.EventError == nil {
		t.Error("Should have error in result data")
	}
}

func TestMessageRequesterFailToBegin(t *testing.T) {
	var receivedTasks []events.Event
	eventBus.HandleEventCreated(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		receivedTasks = append(receivedTasks, event)
	})

	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)

	illId := createIllTrans(t, illRepo, "", "ISIL:REQ4", string(iso18626.TypeActionReceived), "ISIL:RESP4")
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusProblem, events.EventNameMessageRequester)
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

	illId := createIllTrans(t, illRepo, "", "ISIL:REQ4", string(iso18626.TypeActionReceived), "ISIL:RESP4")
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
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

	req := apptest.CreatePeer(t, illRepo, "ISIL:REQ5", "whatever")
	resp := apptest.CreatePeer(t, illRepo, "ISIL:RESP5", "whatever")
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ5", string(iso18626.TypeActionReceived), "ISIL:RESP5")
	apptest.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "ISIL:RESP5", "invalid")
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageRequester)
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

	req := apptest.CreatePeer(t, illRepo, "ISIL:REQ6", "whatever")
	resp := apptest.CreatePeer(t, illRepo, "ISIL:RESP6", "whatever")
	illId := createIllTrans(t, illRepo, req.ID, "ISIL:REQ6", "invalid", "ISIL:RESP6")
	apptest.CreateLocatedSupplier(t, illRepo, illId, resp.ID, "ISIL:RESP6", string(iso18626.TypeStatusLoaned))
	eventId := apptest.GetEventId(t, eventRepo, illId, events.EventTypeTask, events.EventStatusNew, events.EventNameMessageSupplier)
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
		mockResponse   *http.Response
		mockError      error
		expectedResult *iso18626.ISO18626Message
		expectedError  string
	}{
		{
			name: "successful post request",
			url:  "https://test.com",
			msg:  &iso18626.ISO18626Message{},
			mockResponse: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
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
			mockResponse:   nil,
			mockError:      fmt.Errorf("mock request error"),
			expectedResult: nil,
			expectedError:  "mock request error",
		},
		{
			name:           "Marshal error",
			url:            "https://test.com/",
			msg:            nil,
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
			peer := ill_db.Peer{
				Url: tt.url,
			}
			result, err := isoClient.SendHttpPost(&peer, tt.msg)

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

func TestRequestLocallyAvailable(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	reqId := "5636c993-c41c-48f4-a285-170545f6f343"
	data, _ := os.ReadFile("../testdata/request-locally-available.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	httpClient := &http.Client{}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, apptest.CreatePgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusExpectToSupply) &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, string(iso18626.TypeStatusExpectToSupply), illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	selSup, err := illRepo.GetSelectedSupplierForIllTransaction(appCtx, illTrans.ID)
	assert.Nil(t, err)
	selSup.LastStatus = pgtype.Text{
		String: string(iso18626.TypeStatusLoanCompleted),
		Valid:  true,
	}
	selSup, err = illRepo.SaveLocatedSupplier(appCtx, ill_db.SaveLocatedSupplierParams(selSup))
	assert.Nil(t, err)
	_, err = eventBus.CreateNotice(illTrans.ID, events.EventNameRequesterMsgReceived, events.EventData{}, events.EventStatusSuccess)
	assert.Nil(t, err)
	_, err = eventBus.CreateNotice(illTrans.ID, events.EventNameSupplierMsgReceived, events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: &iso18626.ISO18626Message{
				SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
					StatusInfo: iso18626.StatusInfo{
						Status: iso18626.TypeStatusLoaned,
					},
				},
			},
		},
	}, events.EventStatusSuccess)
	assert.Nil(t, err)
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS ISIL:REQ\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS, doNotSend=true\n"+
			"TASK, message-requester = SUCCESS, doNotSend=true\n"+
			"TASK, confirm-supplier-msg = NEW\n",
		apptest.EventsToCompareStringFunc(appCtx, eventRepo, t, illTrans.ID, 8, func(e events.Event) string {
			if e.EventName == "select-supplier" {
				return fmt.Sprintf(apptest.EventRecordFormat+" %v", e.EventType, e.EventName, e.EventStatus, e.ResultData.CustomData["supplierSymbol"])
			}
			return fmt.Sprintf(apptest.EventRecordFormat, e.EventType, e.EventName, e.EventStatus)
		}))
	illTrans, err = illRepo.GetIllTransactionById(appCtx, illTrans.ID)
	assert.Nil(t, err)
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
}

func TestRequestLocallyAvailableT(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	peer, err := illRepo.GetPeerBySymbol(appCtx, "ISIL:REQ") // Peer created by previous test
	assert.Nil(t, err)
	peer.BrokerMode = string(extctx.BrokerModeTranslucent)
	peer, err = illRepo.SavePeer(appCtx, ill_db.SavePeerParams(peer))
	assert.Nil(t, err)
	reqId := "5636c993-c41c-48f4-a285-170545f6f343"
	data, _ := os.ReadFile("../testdata/request-locally-available.xml")
	dataString := strings.Replace(string(data), reqId, reqId+"1", 1)
	reqId = reqId + "1"
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader([]byte(dataString)))
	req.Header.Add("Content-Type", "application/xml")
	httpClient := &http.Client{}
	res, err := httpClient.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}
	var illTrans ill_db.IllTransaction
	test.WaitForPredicateToBeTrue(func() bool {
		illTrans, err = illRepo.GetIllTransactionByRequesterRequestId(appCtx, apptest.CreatePgText(reqId))
		if err != nil {
			t.Errorf("failed to find ill transaction by requester request id %v", reqId)
		}
		return illTrans.LastSupplierStatus.String == string(iso18626.TypeStatusExpectToSupply) &&
			illTrans.LastRequesterAction.String == "Request"
	})
	assert.Equal(t, string(iso18626.TypeStatusExpectToSupply), illTrans.LastSupplierStatus.String)
	assert.Equal(t, "Request", illTrans.LastRequesterAction.String)
	selSup, err := illRepo.GetSelectedSupplierForIllTransaction(appCtx, illTrans.ID)
	assert.Nil(t, err)
	selSup.LastStatus = pgtype.Text{
		String: string(iso18626.TypeStatusLoanCompleted),
		Valid:  true,
	}
	selSup, err = illRepo.SaveLocatedSupplier(appCtx, ill_db.SaveLocatedSupplierParams(selSup))
	assert.Nil(t, err)
	_, err = eventBus.CreateNotice(illTrans.ID, events.EventNameRequesterMsgReceived, events.EventData{}, events.EventStatusSuccess)
	assert.Nil(t, err)
	_, err = eventBus.CreateNotice(illTrans.ID, events.EventNameSupplierMsgReceived, events.EventData{
		CommonEventData: events.CommonEventData{
			IncomingMessage: &iso18626.ISO18626Message{
				SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
					StatusInfo: iso18626.StatusInfo{
						Status: iso18626.TypeStatusLoaned,
					},
				},
			},
		},
	}, events.EventStatusSuccess)
	assert.Nil(t, err)
	assert.Equal(t,
		"NOTICE, request-received = SUCCESS\n"+
			"TASK, locate-suppliers = SUCCESS\n"+
			"TASK, select-supplier = SUCCESS ISIL:REQ\n"+
			"TASK, message-requester = SUCCESS\n"+
			"NOTICE, requester-msg-received = SUCCESS\n"+
			"NOTICE, supplier-msg-received = SUCCESS\n"+
			"TASK, message-supplier = SUCCESS, doNotSend=true\n"+
			"TASK, message-requester = SUCCESS, doNotSend=true\n"+
			"TASK, confirm-supplier-msg = NEW\n",
		apptest.EventsToCompareStringFunc(appCtx, eventRepo, t, illTrans.ID, 8, func(e events.Event) string {
			if e.EventName == "select-supplier" {
				return fmt.Sprintf(apptest.EventRecordFormat+" %v", e.EventType, e.EventName, e.EventStatus, e.ResultData.CustomData["supplierSymbol"])
			}
			return fmt.Sprintf(apptest.EventRecordFormat, e.EventType, e.EventName, e.EventStatus)
		}))
	illTrans, err = illRepo.GetIllTransactionById(appCtx, illTrans.ID)
	assert.Nil(t, err)
	assert.Equal(t, string(iso18626.TypeStatusLoanCompleted), illTrans.LastSupplierStatus.String)
}

func createIllTrans(t *testing.T, illRepo ill_db.IllRepo, requesterId string, requesterSymbol string, action string, supplierSymbol string) string {
	var reqId pgtype.Text
	if requesterId != "" {
		reqId = pgtype.Text{
			String: requesterId,
			Valid:  true,
		}
	}
	illId := uuid.New().String()
	_, err := illRepo.SaveIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID:          illId,
		Timestamp:   test.GetNow(),
		RequesterID: reqId,
		RequesterSymbol: pgtype.Text{
			String: requesterSymbol,
			Valid:  true,
		},
		LastRequesterAction: pgtype.Text{
			String: action,
			Valid:  true,
		},
		RequesterRequestID: pgtype.Text{
			String: uuid.New().String(),
			Valid:  true,
		},
		SupplierSymbol: pgtype.Text{
			String: supplierSymbol,
			Valid:  true,
		},
	})
	if err != nil {
		t.Errorf("Failed to create ILL transaction: %s", err)
	}
	return illId
}
