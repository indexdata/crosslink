package service

import (
	"bytes"
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	tc "github.com/testcontainers/testcontainers-go/modules/compose"
	"github.com/testcontainers/testcontainers-go/wait"
	"net/http"
	"os"
	"testing"
	"time"
)

var eventBus events.EventBus
var illRepo ill_db.IllRepo
var eventRepo events.EventRepo

func TestMain(m *testing.M) {
	ctx := context.Background()
	compose, err := tc.NewDockerCompose("../../docker-compose-test.yml")
	if err != nil {
		panic(fmt.Sprintf("failed to init docker compose: %s", err))
	}
	compose.WaitForService("postgres", wait.ForLog("database system is ready to accept connections").
		WithOccurrence(2).WithStartupTimeout(5*time.Second))
	err = compose.Up(ctx, tc.Wait(true))
	if err != nil {
		panic(fmt.Sprintf("failed to start docker compose: %s", err))
	}

	app.ConnectionString = "postgres://crosslink:crosslink@localhost:35432/crosslink?sslmode=disable"
	app.MigrationsFolder = "file://../../migrations"
	app.HTTP_PORT = 19082
	adapter.MOCK_CLIENT_URL = "http://localhost:19083/iso18626"

	time.Sleep(1 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ = test.StartApp(ctx)

	code := m.Run()

	if err := compose.Down(ctx); err != nil {
		panic(fmt.Sprintf("failed to stop docker compose: %s", err))
	}
	os.Exit(code)
}

func TestLocateSuppliersAndSelect(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})
	var completedSelect []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedSelect = append(completedSelect, event)
	})

	illTrId := getIllTransId(t, illRepo, "sup-test-1")
	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameLocateSuppliers)
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

	supplierId, ok := event.ResultData.Data["supplierId"]
	if !ok || supplierId.(string) == "" {
		t.Error("Expected to have supplierId")
	}
}

func TestLocateSuppliersTaskAlreadyInProgress(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	var completedTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		completedTask = append(completedTask, event)
	})

	illTrId := getIllTransId(t, illRepo, "sup-test-1")
	eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusProcessing, events.EventNameLocateSuppliers)
	err := eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}

	time.Sleep(1 * time.Second)

	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(completedTask) == 0
	}) {
		t.Error("Task was in progress so should not be finished")
	}
}

func TestLocateSuppliersErrors(t *testing.T) {
	tests := []struct {
		name        string
		supReqId    string
		eventStatus events.EventStatus
		message     string
	}{
		{
			name:        "MissingRequestId",
			supReqId:    "",
			eventStatus: events.EventStatusProblem,
			message:     "ill transaction missing SupplierUniqueRecordId",
		},
		{
			name:        "FailedToLocateHoldings",
			supReqId:    "error",
			eventStatus: events.EventStatusError,
			message:     "failed to locate holdings",
		},
		{
			name:        "NoHoldingsFound",
			supReqId:    "h-not-found",
			eventStatus: events.EventStatusProblem,
			message:     "could not find holdings for supplier request id: h-not-found",
		},
		{
			name:        "FailedToGetDirectories",
			supReqId:    "return-error",
			eventStatus: events.EventStatusError,
			message:     "failed to lookup directories: error",
		},
		{
			name:        "NoDirectoriesFound",
			supReqId:    "return-d-not-found",
			eventStatus: events.EventStatusProblem,
			message:     "could not find directories: d-not-found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
			var completedTask []events.Event
			eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
				completedTask = append(completedTask, event)
			})

			illTrId := getIllTransId(t, illRepo, tt.supReqId)
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

			errorMessage, _ := event.ResultData.Data["message"].(string)
			if errorMessage != tt.message {
				t.Errorf("Expected message '%s' got :'%s'", tt.message, errorMessage)
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
			var completedTask []events.Event
			eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
				completedTask = append(completedTask, event)
			})

			illTrId := test.GetIllTransId(t, illRepo)
			eventId := test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameSelectSupplier)
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

			errorMessage, _ := event.ResultData.Data["message"].(string)
			if errorMessage != tt.message {
				t.Errorf("Expected message '%s' got :'%s'", tt.message, errorMessage)
			}
		})
	}
}

func TestSuccessfulFlow(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	var reqNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequestReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		reqNotice = append(reqNotice, event)
	})
	var supMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameSupplierMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		supMsgNotice = append(supMsgNotice, event)
	})
	var reqMsgNotice []events.Event
	eventBus.HandleEventCreated(events.EventNameRequesterMsgReceived, func(ctx extctx.ExtendedContext, event events.Event) {
		reqMsgNotice = append(reqMsgNotice, event)
	})
	var locateTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameLocateSuppliers, func(ctx extctx.ExtendedContext, event events.Event) {
		locateTask = append(locateTask, event)
	})
	var selectTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		selectTask = append(selectTask, event)
	})
	var mesSupTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		mesSupTask = append(mesSupTask, event)
	})
	var mesReqTask []events.Event
	eventBus.HandleTaskCompleted(events.EventNameMessageRequester, func(ctx extctx.ExtendedContext, event events.Event) {
		mesReqTask = append(mesReqTask, event)
	})

	data, _ := os.ReadFile("../testdata/request.xml")
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
		t.Error("should have received 1 request")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(supMsgNotice) == 3
	}) {
		t.Error("should have received 3 supplier messages")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(reqMsgNotice) == 2
	}) {
		t.Error("should have received 2 requester messages")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(locateTask) == 1
	}) {
		t.Error("should have finished locate supplier task")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(selectTask) == 2
	}) {
		t.Error("should have 2 finished select supplier tasks")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesSupTask) == 4
	}) {
		t.Error("should have finished 4 message supplier tasks")
	}
	if !test.WaitForPredicateToBeTrue(func() bool {
		return len(mesReqTask) == 2
	}) {
		t.Error("should have finished 2 message requester tasks")
	}
	illId := mesReqTask[0].IllTransactionID
	illTrans, _ := illRepo.GetIllTransactionById(appCtx, illId)
	if illTrans.LastRequesterAction.String != "ShippedReturn" {
		t.Errorf("ill transaction last requester status should be ShippedReturn not %s",
			illTrans.LastRequesterAction.String)
	}
	suppliers, _ := illRepo.GetLocatedSupplierByIllTransactionAndStatus(appCtx, ill_db.GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: illId,
		SupplierStatus: pgtype.Text{
			String: "selected",
			Valid:  true,
		},
	})

	if suppliers[0].LastStatus.String != "LoanCompleted" {
		t.Errorf("selected supplier last status should be LoanCompleted not %s",
			suppliers[0].LastStatus.String)
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
		t.Errorf("Failed to create ill transaction: %s", err)
	}
	return illId
}
