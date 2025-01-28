package service

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/iso18626"
	"github.com/indexdata/crosslink/broker/test"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"os"
	"strings"
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

	supplier, err := illRepo.GetLocatedSupplierByIllTransactionAndStatus(appCtx, ill_db.GetLocatedSupplierByIllTransactionAndStatusParams{
		IllTransactionID: illTrId,
		SupplierStatus: pgtype.Text{
			String: "new",
			Valid:  true,
		},
	})

	if err != nil {
		t.Error("Failed to get located suppliers " + err.Error())
	}

	if len(supplier) != 1 {
		t.Error("there should be one located supplier")
	}

	// Select Supplier
	eventId = test.GetEventId(t, eventRepo, illTrId, events.EventTypeTask, events.EventStatusNew, events.EventNameSelectSupplier)
	event, _ := eventRepo.GetEvent(appCtx, eventId)
	event.EventData = events.EventData{
		Timestamp:       test.GetNow(),
		ISO18626Message: getIsoMessage("isil:resp1", iso18626.TypeStatusUnfilled),
	}
	_, err = eventRepo.SaveEvent(appCtx, events.SaveEventParams(event))
	if err != nil {
		t.Error("failed to save event " + err.Error())
	}
	err = eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
	if err != nil {
		t.Error("Failed to notify with error " + err.Error())
	}
	var completedSelect []events.Event
	eventBus.HandleTaskCompleted(events.EventNameSelectSupplier, func(ctx extctx.ExtendedContext, event events.Event) {
		completedSelect = append(completedSelect, event)
	})
	if !test.WaitForPredicateToBeTrue(func() bool {
		if len(completedSelect) == 1 {
			event, _ = eventRepo.GetEvent(appCtx, completedSelect[0].ID)
			return event.EventStatus == events.EventStatusSuccess
		}
		return false
	}) {
		t.Error("Expected to have request event received and successfully processed")
	}

	symbol := event.ResultData.Data["supplier"].(string)
	if symbol != "isil:resp1" {
		t.Error("Expected to have supplier isil:resp1 but got " + symbol)
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
			message:     "ill transaction missing supplier request id",
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
	peer := test.CreatePeer(t, illRepo, "isil:resp", "address")
	tests := []struct {
		name        string
		eventStatus events.EventStatus
		message     string
		iso18626M   *iso18626.ISO18626Message
	}{
		{
			name:        "MissingIsoMessage",
			eventStatus: events.EventStatusProblem,
			message:     "event does not have supplying agency message",
			iso18626M:   nil,
		},
		{
			name:        "MissingIsoSupplierMessage",
			eventStatus: events.EventStatusProblem,
			message:     "event does not have supplying agency message",
			iso18626M:   &iso18626.ISO18626Message{},
		},
		{
			name:        "MissingSupplierId",
			eventStatus: events.EventStatusProblem,
			message:     "supplier id is missing in ISO18626 message",
			iso18626M:   getIsoMessage(":", iso18626.TypeStatusUnfilled),
		},
		{
			name:        "IncorrectMessageStatus",
			eventStatus: events.EventStatusProblem,
			message:     "ISO18626 message status incorrect, should be Unfilled but is WillSupply",
			iso18626M:   getIsoMessage("isil:resp", iso18626.TypeStatusWillSupply),
		},
		{
			name:        "NotFoundSupplier",
			eventStatus: events.EventStatusError,
			message:     "could not find supplier by symbol: isil:not_found",
			iso18626M:   getIsoMessage("isil:not_found", iso18626.TypeStatusUnfilled),
		},
		{
			name:        "NotFoundLocatedSupplier",
			eventStatus: events.EventStatusError,
			message:     "could not find located supplier by id: " + peer.ID,
			iso18626M:   getIsoMessage("isil:resp", iso18626.TypeStatusUnfilled),
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
			update, _ := eventRepo.GetEvent(appCtx, eventId)
			update.EventData.Timestamp = test.GetNow()
			update.EventData.ISO18626Message = tt.iso18626M
			_, err := eventRepo.SaveEvent(appCtx, events.SaveEventParams(update))
			if err != nil {
				t.Error("failed to save event " + err.Error())
			}
			err = eventRepo.Notify(appCtx, eventId, events.SignalTaskCreated)
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

func getIllTransId(t *testing.T, illRepo ill_db.IllRepo, supplierRequestId string) string {
	illId := uuid.New().String()
	_, err := illRepo.CreateIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.CreateIllTransactionParams{
		ID:        illId,
		Timestamp: test.GetNow(),
		SupplierRequestID: pgtype.Text{
			String: supplierRequestId,
			Valid:  true,
		},
	})
	if err != nil {
		t.Errorf("Failed to create ill transaction: %s", err)
	}
	return illId
}

func getIsoMessage(supplierSymbol string, status iso18626.TypeStatus) *iso18626.ISO18626Message {
	sup := strings.Split(supplierSymbol, ":")
	return &iso18626.ISO18626Message{
		SupplyingAgencyMessage: &iso18626.SupplyingAgencyMessage{
			Header: iso18626.Header{
				SupplyingAgencyId: iso18626.TypeAgencyId{
					AgencyIdValue: sup[1],
					AgencyIdType: iso18626.TypeSchemeValuePair{
						Text: sup[0],
					},
				},
			},
			StatusInfo: iso18626.StatusInfo{
				Status: status,
			},
		},
	}
}
