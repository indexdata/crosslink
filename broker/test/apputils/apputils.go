package apputils

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/directory"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/test/utils"
	mockapp "github.com/indexdata/crosslink/illmock/app"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
)

const EventRecordFormat = "%v, %v = %v"

func StartApp(ctx context.Context) (events.EventBus, ill_db.IllRepo, events.EventRepo, pr_db.PrRepo) {
	appContext := StartAppReturnContext(ctx)
	return appContext.EventBus, appContext.IllRepo, appContext.EventRepo, appContext.PrRepo
}

func StartAppReturnContext(ctx context.Context) app.Context {
	app.DB_PROVISION = true
	appContext, err := app.Init(ctx)
	utils.Expect(err, "failed to init app")
	go func() {
		err := app.StartServer(appContext)
		utils.Expect(err, "failed to start server")
	}()
	return appContext
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
	_, err := illRepo.SaveIllTransaction(common.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID:        illId,
		Timestamp: GetNow(),
	})
	if err != nil {
		t.Errorf("failed to create ILL transaction: %s", err)
	}
	return illId
}
func GetEventId(t *testing.T, eventRepo events.EventRepo, illId string, eventType events.EventType, status events.EventStatus, eventName events.EventName) string {
	return GetEventIdWithData(t, eventRepo, illId, eventType, status, eventName, events.EventData{})
}

func GetEventIdWithData(t *testing.T, eventRepo events.EventRepo, illId string, eventType events.EventType, status events.EventStatus, eventName events.EventName, data events.EventData) string {
	eventId := uuid.New().String()
	_, err := eventRepo.SaveEvent(common.CreateExtCtxWithArgs(context.Background(), nil), events.SaveEventParams{
		ID:               eventId,
		IllTransactionID: illId,
		Timestamp:        GetNow(),
		EventType:        eventType,
		EventName:        eventName,
		EventStatus:      status,
		EventData:        data,
		LastSignal:       string(events.SignalTaskCreated),
		PatronRequestID:  events.DEFAULT_PATRON_REQUEST_ID,
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
	return CreatePeerWithModeAndVendor(t, illRepo, symbol, url, brokerMode, directory.ReShare, directory.Entry{})
}

func CreatePeerWithModeAndVendor(t *testing.T, illRepo ill_db.IllRepo, symbol string, url string, brokerMode string, vendor directory.EntryVendor, customData directory.Entry) ill_db.Peer {
	peer, err := illRepo.SavePeer(common.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SavePeerParams{
		ID:            uuid.New().String(),
		Name:          symbol,
		Url:           url,
		RefreshPolicy: ill_db.RefreshPolicyNever,
		RefreshTime: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
		BrokerMode: brokerMode,
		Vendor:     string(vendor),
		CustomData: customData,
	})
	if err != nil {
		t.Errorf("Failed to create peer: %s", err)
	}
	_, err = illRepo.SaveSymbol(common.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveSymbolParams{
		SymbolValue: symbol,
		PeerID:      peer.ID,
	})
	if err != nil {
		t.Errorf("Failed to create symbol: %s", err)
	}
	return peer
}

func CreateLocatedSupplier(t *testing.T, illRepo ill_db.IllRepo, illTransId string, supplierId string, supplierSymbol string, status string) ill_db.LocatedSupplier {
	supplier, err := illRepo.SaveLocatedSupplier(common.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveLocatedSupplierParams{
		ID:               uuid.New().String(),
		IllTransactionID: illTransId,
		SupplierID:       supplierId,
		SupplierSymbol:   supplierSymbol,
		Ordinal:          0,
		SupplierStatus:   ill_db.SupplierStateSelectedPg,
		LastStatus: pgtype.Text{
			String: status,
			Valid:  status != "",
		},
		LastAction: pgtype.Text{
			String: string(ill_db.RequestAction),
			Valid:  true,
		},
	})
	if err != nil {
		t.Errorf("Failed to create peer: %s", err)
	}
	return supplier
}

func EventsCompareString(appCtx common.ExtendedContext, eventRepo events.EventRepo, t *testing.T, illId string, expected string) {
	actual := eventsToCompareString(appCtx, eventRepo, t, illId, strings.Count(expected, "\n"))
	assert.Equal(t, expected, actual)
}

func eventsToCompareString(appCtx common.ExtendedContext, eventRepo events.EventRepo, t *testing.T, illId string, messageCount int) string {
	return EventsToCompareStringFunc(appCtx, eventRepo, t, illId, messageCount, false, func(e events.Event) string {
		return fmt.Sprintf(EventRecordFormat, e.EventType, e.EventName, e.EventStatus)
	})
}

func EventsToCompareStringFunc(appCtx common.ExtendedContext, eventRepo events.EventRepo, t *testing.T, illId string, messageCount int, ignoreState bool, eventFmt func(events.Event) string) string {
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
		if !ignoreState {
			for _, e := range eventList {
				if e.EventStatus == events.EventStatusProcessing || e.EventStatus == events.EventStatusNew {
					appCtx.Logger().Info("Check events processing state")
					return false
				}
			}
		}
		return true
	})

	value := ""
	for _, e := range eventList {
		value = value + eventFmt(e)
		if e.EventStatus == events.EventStatusProblem && e.ResultData.Problem != nil {
			value += ", problem=" + e.ResultData.Problem.Kind
		}
		if e.EventStatus == events.EventStatusError {
			value += ", error=" + e.ResultData.EventError.Message
		}
		if doNotSendValue, ok := e.ResultData.CustomData[common.DO_NOT_SEND].(bool); doNotSendValue && ok {
			value += ", doNotSend=true"
		}
		value += "\n"
	}
	return value
}

func StartMockApp(mockPort int) {
	utils.Expect(os.Setenv("HTTP_PORT", strconv.Itoa(mockPort)), "failed to set mock server port")

	go func() {
		var mockApp mockapp.MockApp
		utils.Expect(mockApp.Run(), "failed to start illmock server")
	}()
	utils.WaitForServiceUp(mockPort)
}
