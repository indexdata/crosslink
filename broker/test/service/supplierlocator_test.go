package service

import (
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
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"os"
	"testing"
	"time"
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
	adapter.MOCK_SUPPLIER_PORT = "19082"

	time.Sleep(1 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	eventBus, illRepo, eventRepo, _ = test.StartApp(ctx)

	code := m.Run()

	if err := pgContainer.Terminate(ctx); err != nil {
		panic(fmt.Sprintf("failed to stop db container: %s", err))
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
