package service

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/test"
	mockapp "github.com/indexdata/crosslink/illmock/app"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/go-utils/utils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
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
	mockPort := strconv.Itoa(utils.Must(test.GetFreePort()))
	app.HTTP_PORT = utils.Must(test.GetFreePort())
	test.Expect(os.Setenv("HTTP_PORT", mockPort), "failed to set mock client port")
	test.Expect(os.Setenv("PEER_URL", "http://localhost:"+strconv.Itoa(app.HTTP_PORT)+"/iso18626"), "failed to set peer URL")

	go func() {
		var mockApp mockapp.MockApp
		test.Expect(mockApp.Run(), "failed to start illmock client")
	}()
	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	app.FORWARD_WILL_SUPPLY = true
	adapter.MOCK_CLIENT_URL = "http://localhost:" + mockPort + "/iso18626"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo = test.StartApp(ctx)
	test.WaitForServiceUp(app.HTTP_PORT)

	code := m.Run()

	test.Expect(pgContainer.Terminate(ctx), "failed to stop db container")
	os.Exit(code)
}

func TestLocateSuppliersAndSelect(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := getIllTransId(t, illRepo, "sup-test-1")
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			completedTask = append(completedTask, event)
		}
	})
	var completedSelect []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			completedSelect = append(completedSelect, event)
		}
	})
	yesterday := time.Now().Add(-24 * time.Hour)
	toChange, err := illRepo.SavePeer(appCtx, ill_db.SavePeerParams{
		ID:            uuid.New().String(),
		Symbol:        "ISIL:SUP1",
		Name:          "ISIL:SUP1",
		RefreshPolicy: ill_db.RefreshPolicyTransaction,
		RefreshTime: pgtype.Timestamp{
			Time:  yesterday,
			Valid: true,
		},
		Url: "http://should-change.com",
	},
	)
	if err != nil {
		t.Error("Failed to create peer " + err.Error())
	}
	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameLocateSuppliers)
	err = eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
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

	var event events.Event
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedSelect) == 1 {
			event, _ = eventRepo.GetEvent(appCtx, completedSelect[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}

	supplierId, ok := event.ResultData.CustomData["supplierId"]
	if !ok || supplierId.(string) == "" {
		t.Fatal("Expected to have supplierId")
	}
	selectedPeer, err := illRepo.GetPeerById(appCtx, supplierId.(string))
	if err != nil {
		t.Error("Failed to get selected peer " + err.Error())
	}
	if selectedPeer.Url == toChange.Url {
		t.Error("Peer entry should be updated")
	}
}

func TestLocateSuppliersNoUpdate(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	var completedTask []events.Event
	illTrId := getIllTransId(t, illRepo, "return-ISIL:NOCHANGE")
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if event.IllTransactionID == illTrId {
			completedTask = append(completedTask, event)
		}
	})
	var completedSelect []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if event.IllTransactionID == illTrId {
			completedSelect = append(completedSelect, event)
		}
	})

	noChange, err := illRepo.SavePeer(appCtx, ill_db.SavePeerParams{
		ID:            uuid.New().String(),
		Symbol:        "ISIL:NOCHANGE",
		Name:          "No Change",
		RefreshPolicy: ill_db.RefreshPolicyNever,
		RefreshTime: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		Url: "http://no-change.com",
	},
	)
	if err != nil {
		t.Error("Failed to create peer " + err.Error())
	}
	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameLocateSuppliers)
	err = eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
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

	var event events.Event
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedSelect) == 1 {
			event, _ = eventRepo.GetEvent(appCtx, completedSelect[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}

	supplierId, ok := event.ResultData.CustomData["supplierId"]
	if !ok || supplierId.(string) == "" {
		t.Error("Expected to have supplierId")
	}
	selectedPeer, err := illRepo.GetPeerById(appCtx, supplierId.(string))
	if err != nil {
		t.Error("Failed to get selected peer " + err.Error())
	}
	if selectedPeer.Url != noChange.Url {
		t.Error("Peer entry should not be updated")
	}
}

func TestLocateSuppliersOrder(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := getIllTransId(t, illRepo, "LOANED;LOANED")
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			completedTask = append(completedTask, event)
		}
	})
	sup1 := getOrCreatePeer(t, illRepo, "ISIL:SUP1", 3, 4)
	sup2 := getOrCreatePeer(t, illRepo, "ISIL:SUP2", 2, 4)

	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameLocateSuppliers)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}
	var event events.Event
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ = eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	if supId := getSupplierId(0, event.ResultData.CustomData); supId != sup2.ID {
		t.Errorf("Expected to sup2 be first supplier, expected %s, got %s", sup2.ID, supId)
	}
	if supId := getSupplierId(1, event.ResultData.CustomData); supId != sup1.ID {
		t.Error("Expected to sup1 be second supplier")
	}
	// Clean
	getOrCreatePeer(t, illRepo, "ISIL:SUP1", 0, 0)
	getOrCreatePeer(t, illRepo, "ISIL:SUP2", 0, 0)
}

func TestLocateSupplierUnreachable(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := getIllTransId(t, illRepo, "LOANED;LOANED")
	illTr, err := illRepo.GetIllTransactionById(appCtx, illTrId)
	if err != nil {
		t.Error("failed to get ill transaction by id: " + err.Error())
	}
	illTr.LastRequesterAction = pgtype.Text{
		String: "Request",
		Valid:  true,
	}
	illTr, err = illRepo.SaveIllTransaction(appCtx, ill_db.SaveIllTransactionParams(illTr))
	if err != nil {
		t.Error("failed to update ill transaction: " + err.Error())
	}
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			completedTask = append(completedTask, event)
		}
	})
	var messageSupplier []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			messageSupplier = append(messageSupplier, event)
		}
	})
	var completedSelect []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			completedSelect = append(completedSelect, event)
		}
	})

	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameLocateSuppliers)
	err = eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}
	var event events.Event
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ = eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedSelect) >= 2 {
			event, _ = eventRepo.GetEvent(appCtx, completedSelect[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("expected to have select supplier event twice and successful")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(messageSupplier) > 0 {
			event, _ = eventRepo.GetEvent(appCtx, messageSupplier[0].ID)
			return event.EventStatus == events.EventStatusProblem
		}
		return false
	}) {
		t.Error("expected to have failed request to supplier")
	}
}

func TestLocateSuppliersTaskAlreadyInProgress(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := getIllTransId(t, illRepo, "sup-test-1")
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			completedTask = append(completedTask, event)
		}
	})

	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusProcessing, events.EventNameLocateSuppliers)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("failed to notify with error " + err.Error())
	}

	time.Sleep(1 * time.Second)

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(completedTask) == 0
	}) {
		t.Error("task was in progress so should not be finished")
	}
}

func TestLocateSuppliersErrors(t *testing.T) {
	tests := []struct {
		name        string
		supReqId    string
		eventStatus events.EventStatus
		message     string
		problem     string
	}{
		{
			name:        "MissingRequestId",
			supReqId:    "",
			eventStatus: events.EventStatusProblem,
			problem:     "ILL transaction missing SupplierUniqueRecordId",
		},
		{
			name:        "FailedToLocateHoldings",
			supReqId:    "error",
			eventStatus: events.EventStatusError,
			message:     "failed to locate holdings",
		},
		{
			name:        "NoHoldingsFound",
			supReqId:    "not-found",
			eventStatus: events.EventStatusProblem,
			problem:     "could not find holdings for supplier request id: not-found",
		},
		{
			name:        "FailedToGetDirectories",
			supReqId:    "return-error",
			eventStatus: events.EventStatusProblem,
			problem:     "failed to add any supplier from: error",
		},
		{
			name:        "NoDirectoriesFound",
			supReqId:    "return-d-not-found",
			eventStatus: events.EventStatusProblem,
			problem:     "failed to add any supplier from: d-not-found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
			illTrId := getIllTransId(t, illRepo, tt.supReqId)
			var completedTask []events.Event
			eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
				if illTrId == event.IllTransactionID {
					completedTask = append(completedTask, event)
				}
			})
			var messageRequester []events.Event
			eventBus.HandleEventCreated(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
				if illTrId == event.IllTransactionID {
					messageRequester = append(messageRequester, event)
				}
			})

			eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameLocateSuppliers)
			err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
			if err != nil {
				t.Error("Failed to notify with error " + err.Error())
			}

			var event events.Event
			if !test.WaitForPredicateToBeTrue(func() bool {
				if len(completedTask) == 1 {
					event, _ = eventRepo.GetEvent(appCtx, completedTask[0].ID)
					return event.EventStatus == tt.eventStatus
				}
				return false
			}) {
				t.Error("Expected to have request event received and processed")
			}

			if tt.message != "" {
				if event.ResultData.EventError.Message != tt.message {
					t.Errorf("Expected message '%s' got :'%s'", tt.message, event.ResultData.EventError.Message)
				}
			}

			if tt.problem != "" {
				if event.ResultData.Problem.Details != tt.problem {
					t.Errorf("Expected error message '%s' got :'%v'", tt.message, event.ResultData.Problem.Details)
				}
			}

			if !test.WaitForPredicateToBeTrue(func() bool {
				return len(messageRequester) == 1
			}) {
				t.Error("expected to have unfilled message send to requester")
			}
		})
	}
}

func TestSelectSupplierErrors(t *testing.T) {
	tests := []struct {
		name        string
		eventStatus events.EventStatus
		message     string
	}{
		{
			name:        "NotFoundLocatedSupplier",
			eventStatus: events.EventStatusProblem,
			message:     "no suppliers with new status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
			illTrId := test.GetIllTransId(t, illRepo)
			var completedTask []events.Event
			eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
				if illTrId == event.IllTransactionID {
					completedTask = append(completedTask, event)
				}
			})
			var messageRequester []events.Event
			eventBus.HandleEventCreated(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
				if illTrId == event.IllTransactionID {
					messageRequester = append(messageRequester, event)
				}
			})

			eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameSelectSupplier)
			err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
			if err != nil {
				t.Error("failed to notify with error " + err.Error())
			}

			var event events.Event
			if !test.WaitForPredicateToBeTrue(func() bool {
				if len(completedTask) == 1 {
					event, _ = eventRepo.GetEvent(appCtx, completedTask[0].ID)
					return event.EventStatus == tt.eventStatus
				}
				return false
			}) {
				t.Error("expected to have request event received and processed")
			}

			if event.ResultData.Problem.Details != tt.message {
				t.Errorf("expected message '%s' got :'%v'", tt.message, event.ResultData.Problem.Details)
			}

			if !test.WaitForPredicateToBeTrue(func() bool {
				return len(messageRequester) == 1
			}) {
				t.Error("expected to have unfilled message send to requester")
			}
		})
	}
}

func TestCreatePeerFromDirectoryResponse(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	supSymbol := "ISIL:NEWSUPPLIER" + uuid.NewString()
	illTrId := getIllTransId(t, illRepo, "return-"+supSymbol)
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			completedTask = append(completedTask, event)
		}
	})

	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameLocateSuppliers)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("failed to notify with error " + err.Error())
	}

	var event events.Event
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedTask) == 1 {
			event, _ = eventRepo.GetEvent(appCtx, completedTask[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("expected to have request event received and processed")
	}

	_, err = illRepo.GetPeerBySymbol(appCtx, supSymbol)
	if err != nil {
		t.Error("expected to have new peer created")
	}
}

func TestSuccessfulFlow(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := ""
	var reqNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		reqNotice = append(reqNotice, event)
		illTrId = event.IllTransactionID
	})
	var supMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			supMsgNotice = append(supMsgNotice, event)
		}
	})
	var reqMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			reqMsgNotice = append(reqMsgNotice, event)
		}
	})
	var locateTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			locateTask = append(locateTask, event)
		}
	})
	var selectTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			selectTask = append(selectTask, event)
		}
	})
	var mesSupTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			mesSupTask = append(mesSupTask, event)
		}
	})
	var mesReqTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			mesReqTask = append(mesReqTask, event)
		}
	})

	data, _ := os.ReadFile("../testdata/request-ok.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(reqNotice) == 1
	}) {
		t.Errorf("should have received 1 request, but got %d", len(reqNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(supMsgNotice) == 5
	}) {
		t.Errorf("should have received 3 supplier messages, but got %d", len(supMsgNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(reqMsgNotice) == 2
	}) {
		t.Errorf("should have received 2 requester messages, but got %d", len(reqMsgNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(locateTask) == 1
	}) {
		t.Errorf("should have finished locate supplier task, but got %d", len(locateTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(selectTask) == 2
	}) {
		t.Errorf("should have 2 finished select supplier tasks, but got %d", len(selectTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesSupTask) == 4
	}) {
		t.Errorf("should have finished 4 message supplier tasks, but got %d", len(mesSupTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesReqTask) == 4
	}) {
		t.Errorf("should have finished 2 message requester tasks, but got %d", len(mesReqTask))
	}
	illId := mesReqTask[0].IllTransactionID
	illTrans, _ := illRepo.GetIllTransactionById(appCtx, illId)
	if illTrans.LastRequesterAction.String != "ShippedReturn" {
		t.Errorf("ILL transaction last requester status should be ShippedReturn not %s",
			illTrans.LastRequesterAction.String)
	}
	supplier, _ := illRepo.GetSelectedSupplierForIllTransaction(appCtx, illTrans.ID)

	if supplier.LastStatus.String != "LoanCompleted" {
		t.Errorf("selected supplier last status should be LoanCompleted not %s",
			supplier.LastStatus.String)
	}
}

func TestLoanedOverdue(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := ""
	var reqNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		reqNotice = append(reqNotice, event)
		illTrId = event.IllTransactionID
	})
	var supMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			supMsgNotice = append(supMsgNotice, event)
		}
	})
	var reqMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			reqMsgNotice = append(reqMsgNotice, event)
		}
	})
	var locateTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			locateTask = append(locateTask, event)
		}
	})
	var selectTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			selectTask = append(selectTask, event)
		}
	})
	var mesSupTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			mesSupTask = append(mesSupTask, event)
		}
	})
	var mesReqTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			mesReqTask = append(mesReqTask, event)
		}
	})

	data, _ := os.ReadFile("../testdata/request-loaned-overdue.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(reqNotice) == 1
	}) {
		t.Errorf("should have received 1 request, but got %d", len(reqNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(supMsgNotice) == 3
	}) {
		t.Errorf("should have received 3 supplier messages, but got %d", len(supMsgNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(reqMsgNotice) == 2
	}) {
		t.Errorf("should have received 2 requester messages, but got %d", len(reqMsgNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(locateTask) == 1
	}) {
		t.Errorf("should have finished locate supplier task, but got %d", len(locateTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(selectTask) == 1
	}) {
		t.Errorf("should have 1 finished select supplier tasks, but got %d", len(selectTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesSupTask) == 3
	}) {
		t.Errorf("should have finished 3 message supplier tasks, but got %d", len(mesSupTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesReqTask) == 3
	}) {
		t.Errorf("should have finished 3 message requester tasks, but got %d", len(mesReqTask))
	}
	illId := mesReqTask[0].IllTransactionID
	illTrans, _ := illRepo.GetIllTransactionById(appCtx, illId)
	if illTrans.LastRequesterAction.String != "ShippedReturn" {
		t.Errorf("ILL transaction last requester status should be ShippedReturn not %s",
			illTrans.LastRequesterAction.String)
	}
	supplier, _ := illRepo.GetSelectedSupplierForIllTransaction(appCtx, illTrans.ID)

	if supplier.LastStatus.String != "LoanCompleted" {
		t.Errorf("selected supplier last status should be LoanCompleted not %s",
			supplier.LastStatus.String)
	}
}

func TestRetryLoaned(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := ""
	var reqNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		reqNotice = append(reqNotice, event)
		illTrId = event.IllTransactionID
	})
	var supMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			supMsgNotice = append(supMsgNotice, event)
		}
	})
	var reqMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			reqMsgNotice = append(reqMsgNotice, event)
		}
	})
	var locateTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			locateTask = append(locateTask, event)
		}
	})
	var selectTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			selectTask = append(selectTask, event)
		}
	})
	var mesSupTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			mesSupTask = append(mesSupTask, event)
		}
	})
	var mesReqTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		if illTrId == event.IllTransactionID {
			mesReqTask = append(mesReqTask, event)
		}
	})

	data, _ := os.ReadFile("../testdata/request-retry-loaned.xml")
	req, _ := http.NewRequest("POST", adapter.MOCK_CLIENT_URL, bytes.NewReader(data))
	req.Header.Add("Content-Type", "application/xml")
	client := &http.Client{}
	res, err := client.Do(req)
	if err != nil {
		t.Errorf("failed to send request to mock :%s", err)
	}
	if res.StatusCode != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v",
			res.StatusCode, http.StatusOK)
	}

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(reqNotice) == 1
	}) {
		t.Errorf("should have received 1 request, but got %d", len(reqNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(supMsgNotice) == 3
	}) {
		t.Errorf("should have received 3 supplier messages, but got %d", len(supMsgNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(reqMsgNotice) == 3
	}) {
		t.Errorf("should have received 3 requester messages, but got %d", len(reqMsgNotice))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(locateTask) == 1
	}) {
		t.Errorf("should have 1 finished locate supplier task, but got %d", len(locateTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(selectTask) == 1
	}) {
		t.Errorf("should have 1 finished select supplier tasks, but got %d", len(selectTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesSupTask) == 4
	}) {
		t.Errorf("should have finished 4 message supplier tasks, but got %d", len(mesSupTask))
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesReqTask) == 3
	}) {
		t.Errorf("should have finished 3 message requester tasks, but got %d", len(mesReqTask))
	}
	illId := mesReqTask[1].IllTransactionID
	illTrans, _ := illRepo.GetIllTransactionById(appCtx, illId)
	if illTrans.LastRequesterAction.String != "ShippedReturn" {
		t.Errorf("ILL transaction last requester status should be ShippedReturn not %s",
			illTrans.LastRequesterAction.String)
	}
	supplier, _ := illRepo.GetSelectedSupplierForIllTransaction(appCtx, illTrans.ID)

	if supplier.LastStatus.String != "LoanCompleted" {
		t.Errorf("selected supplier last status should be LoanCompleted not %s",
			supplier.LastStatus.String)
	}
}

func getIllTransId(t *testing.T, illRepo ill_db.IllRepo, supplierRecordId string) string {
	data := ill_db.IllTransactionData{
		BibliographicInfo: iso18626.BibliographicInfo{
			SupplierUniqueRecordId: supplierRecordId,
		},
	}
	illId := uuid.New().String()
	_, err := illRepo.SaveIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID:                 illId,
		Timestamp:          test.GetNow(),
		IllTransactionData: data,
	})
	if err != nil {
		t.Errorf("Failed to create ILL transaction: %s", err)
	}
	return illId
}

func getOrCreatePeer(t *testing.T, illRepo ill_db.IllRepo, symbol string, loans int, borrows int) ill_db.Peer {
	ctx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	peer, err := illRepo.GetPeerBySymbol(ctx, symbol)
	if err != nil {
		peer, err := illRepo.SavePeer(ctx, ill_db.SavePeerParams{
			ID:            uuid.NewString(),
			Symbol:        symbol,
			Name:          symbol,
			RefreshPolicy: ill_db.RefreshPolicyTransaction,
			RefreshTime: pgtype.Timestamp{
				Time:  time.Now().Add(-24 * time.Hour),
				Valid: true,
			},
			Url:          adapter.MOCK_CLIENT_URL,
			LoansCount:   service.ToInt32(loans),
			BorrowsCount: service.ToInt32(borrows),
		})
		if err != nil {
			t.Errorf("Failed to save peer: %s", err)
		}
		return peer
	} else {
		peer.LoansCount = service.ToInt32(loans)
		peer.BorrowsCount = service.ToInt32(borrows)
		peer, err := illRepo.SavePeer(ctx, ill_db.SavePeerParams(peer))
		if err != nil {
			t.Errorf("Failed to update peer: %s", err)
		}
		return peer
	}
}

func getSupplierId(i int, result map[string]interface{}) string {
	suppliers, ok := result["suppliers"]
	if ok {
		record := suppliers.([]interface{})[i]
		supId, ok := record.(map[string]interface{})["SupplierID"]
		if ok {
			return supId.(string)
		}
	}
	return ""
}
