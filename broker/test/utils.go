package test

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/client"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/jackc/pgx/v5/pgtype"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

func GetNow() pgtype.Timestamp {
	return pgtype.Timestamp{
		Time:  time.Now(),
		Valid: true,
	}
}

func WaitForPredicateToBeTrue(predicate func() bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ticker := time.NewTicker(20 * time.Millisecond) // Check every 20ms
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

func StartApp(ctx context.Context) (events.EventBus, ill_db.IllRepo, events.EventRepo, client.Iso18626Client) {
	var eventBus events.EventBus
	var illRepo ill_db.IllRepo
	var eventRepo events.EventRepo
	var iso18626Client client.Iso18626Client
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		app.RunMigrateScripts()
		pool := app.InitDbPool()
		eventRepo = app.CreateEventRepo(pool)
		eventBus = app.CreateEventBus(eventRepo)
		illRepo = app.CreateIllRepo(pool)
		iso18626Client = client.CreateIso18626Client(eventBus, illRepo)
		supplierLocator := service.CreateSupplierLocator(eventBus, illRepo, new(adapter.MockDirectoryLookupAdapter), new(adapter.MockHoldingsLookupAdapter))
		workflowManager := service.CreateWorkflowManager(eventBus)
		app.AddDefaultHandlers(eventBus, iso18626Client, supplierLocator, workflowManager)
		app.StartEventBus(ctx, eventBus)
		wg.Done()
		app.StartServer(illRepo, eventBus)
	}()
	wg.Wait()
	return eventBus, illRepo, eventRepo, iso18626Client
}

func GetIllTransId(t *testing.T, illRepo ill_db.IllRepo) string {
	illId := uuid.New().String()
	_, err := illRepo.SaveIllTransaction(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveIllTransactionParams{
		ID:        illId,
		Timestamp: GetNow(),
	})
	if err != nil {
		t.Errorf("Failed to create ill transaction: %s", err)
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
	})

	if err != nil {
		t.Errorf("Failed to create event: %s", err)
	}
	return eventId
}

func CreatePeer(t *testing.T, illRepo ill_db.IllRepo, symbol string, address string) ill_db.Peer {
	peer, err := illRepo.CreatePeer(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.CreatePeerParams{
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

func CreateLocatedSupplier(t *testing.T, illRepo ill_db.IllRepo, illTransId string, supplierId string) ill_db.LocatedSupplier {
	supplier, err := illRepo.SaveLocatedSupplier(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SaveLocatedSupplierParams{
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

func Expect(err error, message string) {
	if err != nil {
		panic(fmt.Sprintf(message+" Errror : %s", err))
	}
}

// GetFreePort asks the kernel for a free open port that is ready to use.
func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	// release for now so it can be bound by the actual server
	// a more robust solution would be to bind the server to the port and close it here
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func WaitForServiceUp(port int) {
	if !WaitForPredicateToBeTrue(func() bool {
		resp, err := http.Get("http://localhost:" + strconv.Itoa(port) + "/healthz")
		if err != nil {
			return false
		}
		return resp.StatusCode == http.StatusOK
	}) {
		panic("failed to start broker")
	} else {
		fmt.Println("Service up")
	}
}
