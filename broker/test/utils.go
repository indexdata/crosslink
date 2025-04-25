package test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	extctx "github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/jackc/pgx/v5/pgtype"
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

func StartApp(ctx context.Context) (events.EventBus, ill_db.IllRepo, events.EventRepo) {
	context, err := app.Init(ctx)
	Expect(err, "failed to init app")
	go func() {
		err := app.StartServer(context)
		Expect(err, "failed to start server")
	}()
	return context.EventBus, context.IllRepo, context.EventRepo
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
	})

	if err != nil {
		t.Errorf("Failed to create event: %s", err)
	}
	return eventId
}

func CreatePeer(t *testing.T, illRepo ill_db.IllRepo, symbol string, url string) ill_db.Peer {
	peer, err := illRepo.SavePeer(extctx.CreateExtCtxWithArgs(context.Background(), nil), ill_db.SavePeerParams{
		ID:            uuid.New().String(),
		Name:          symbol,
		Url:           url,
		RefreshPolicy: ill_db.RefreshPolicyNever,
		RefreshTime: pgtype.Timestamp{
			Time:  time.Now(),
			Valid: true,
		},
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

func CreatePgText(value string) pgtype.Text {
	textValue := pgtype.Text{
		String: value,
		Valid:  true,
	}
	return textValue
}
