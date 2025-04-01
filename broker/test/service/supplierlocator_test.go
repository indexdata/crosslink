package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/test"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
)

var eventBus events.EventBus
var illRepo ill_db.IllRepo
var eventRepo events.EventRepo

func TestLocateSuppliersAndSelect(t *testing.T) {
	appCtx := extctx.CreateExtCtxWithArgs(context.Background(), nil)
	illTrId := getIllTransId(t, illRepo, "return-ISIL:SUP-TEST-1")
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
		Name:          "ISIL:SUP-TEST-1",
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
	_, err = illRepo.SaveSymbol(appCtx, ill_db.SaveSymbolParams{
		SymbolValue: "ISIL:SUP-TEST-1",
		PeerID:      toChange.ID,
	})
	if err != nil {
		t.Error("Failed to create symbol " + err.Error())
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
	_, err = illRepo.SaveSymbol(appCtx, ill_db.SaveSymbolParams{
		SymbolValue: "ISIL:NOCHANGE",
		PeerID:      noChange.ID,
	})
	if err != nil {
		t.Error("Failed to create symbol " + err.Error())
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

		_, err = illRepo.SaveSymbol(ctx, ill_db.SaveSymbolParams{
			SymbolValue: symbol,
			PeerID:      peer.ID,
		})
		if err != nil {
			t.Error("Failed to create symbol " + err.Error())
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
