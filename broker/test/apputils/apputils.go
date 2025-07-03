package apputils

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/utils"
	"github.com/jackc/pgx/v5/pgtype"
)

const EventRecordFormat = "%v, %v = %v"

func StartApp(ctx context.Context) (events.EventBus, ill_db.IllRepo, events.EventRepo) {
	context, err := app.Init(ctx)
	utils.Expect(err, "failed to init app")
	go func() {
		err := app.StartServer(context)
		utils.Expect(err, "failed to start server")
	}()
	return context.EventBus, context.IllRepo, context.EventRepo
}

func CreatePgText(value string) pgtype.Text {
	textValue := pgtype.Text{
		String: value,
		Valid:  true,
	}
	return textValue
}

func GetNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}

func GetIllTransId(t *testing.T, illRepo ill_db.IllRepo) string {
	illId := uuid.New().String()
	_, err := illRepo.SaveIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID:        illId,
		Timestamp: GetNow(),
	})
	if err != nil {
		t.Errorf("failed to create ILL transaction: %s", err)
	}
	return illId
}

func GetEventId(t *testing.T, eventRepo events.EventRepo, illId string, eventType events.EventType, status events.EventStatus, eventName events.EventName) string {
	eventId := uuid.New().String()
	_, err := eventRepo.SaveEvent(extctx.CreateExtCtxWithArgs(context.Background(), nil), events.SaveEventParams{
		ID:               eventId,
		IllTransactionID: illId,
		Timestamp:        GetNow(),
		EventType:        eventType,
		EventName:        eventName,
		EventStatus:      status,
		EventData:        events.EventData{},
		LastSignal:       string(events.SignalTaskCreated),
	})

	if err != nil {
		t.Errorf("Failed to create event: %s", err)
	}
	return eventId
}

func CreatePeer(t *testing.T, illRepo ill_db.IllRepo, symbol string, url string) ill_db.Peer {
	return CreatePeerWithMode(t, illRepo, symbol, url, app.BROKER_MODE)
}

func CreatePeerWithMode(t *testing.T, illRepo ill_db.IllRepo, symbol string, url string, brokerMode string) ill_db.Peer {
	peer, err := illRepo.SavePeer(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SavePeerParams{
		ID:            uuid.New().String(),
		Name:          symbol,
		Url:           url,
		RefreshPolicy: ill_db.RefreshPolicyNever,
		RefreshTime: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		BrokerMode: brokerMode,
	})
	if err != nil {
		t.Errorf("Failed to create peer: %s", err)
	}
	_, err = illRepo.SaveSymbol(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveSymbolParams{
		SymbolValue: symbol,
		PeerID:      peer.ID,
	})
	if err != nil {
		t.Errorf("Failed to create symbol: %s", err)
	}
	return peer
}

func CreateLocatedSupplier(t *testing.T, illRepo ill_db.IllRepo, illTransId string, supplierId string, supplierSymbol string, status string) ill_db.LocatedSupplier {
	supplier, err := illRepo.SaveLocatedSupplier(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: illTransId,
		SupplierID:       supplierId,
		SupplierSymbol:   supplierSymbol,
		Ordinal:          0,
		SupplierStatus:   ill_db.SupplierStatusSelectedPg,
		LastStatus: pgtype.Text{
			String: status,
			Valid:  status != "",
		},
	})
	if err != nil {
		t.Errorf("Failed to create peer: %s", err)
	}
	return supplier
}

func EventsToCompareString(appCtx extctx.ExtendedContext, eventRepo events.EventRepo, t *testing.T, illId string, messageCount int) string {
	return EventsToCompareStringFunc(appCtx, eventRepo, t, illId, messageCount, func(e events.Event) string {
		return fmt.Sprintf(EventRecordFormat, e.EventType, e.EventName, e.EventStatus)
	})
}

func EventsToCompareStringFunc(appCtx extctx.ExtendedContext, eventRepo events.EventRepo, t *testing.T, illId string, messageCount int, eventFmt func(events.Event) string) string {
	var eventList []events.Event
	var err error

	utils.WaitForPredicateToBeTrue(func() bool {
		eventList, _, err = eventRepo.GetIllTransactionEvents(appCtx, illId)
		if err != nil {
			t.Errorf("failed to find events for ill transaction id %v", illId)
		}
		if len(eventList) != messageCount {
			appCtx.Logger().Info("Check events count " + strconv.Itoa(len(eventList)))
			return false
		}
		for _, e := range eventList {
			if e.EventStatus == events.EventStatusProcessing || e.EventStatus == events.EventStatusNew {
				appCtx.Logger().Info("Check events processing state")
				return false
			}
		}
		return true
	})

	value := ""
	for _, e := range eventList {
		value = value + eventFmt(e)
		if e.EventStatus == events.EventStatusProblem {
			value += ", problem=" + e.ResultData.Problem.Kind
		}
		if e.EventStatus == events.EventStatusError {
			value += ", error=" + e.ResultData.EventError.Message
		}
		value += "\n"
	}
	return value
}
