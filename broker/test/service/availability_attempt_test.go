package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/client"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/broker/service"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func availabilityAttemptSelection(t *testing.T, ctx common.ExtendedContext) ill_db.LocatedSupplier {
	t.Helper()
	peer, err := illRepo.SavePeer(ctx, ill_db.SavePeerParams{ID: uuid.NewString(), Name: "attempt test", Url: "https://supplier.invalid", RefreshPolicy: ill_db.RefreshPolicyNever, RefreshTime: pgtype.Timestamp{Time: time.Now(), Valid: true}, CustomData: dirapi.Entry{
		CatalogConfig: &dirapi.CatalogConfig{Sru: &dirapi.SruConfig{Address: "https://catalog.invalid/sru"}}, HoldingsPolicy: &dirapi.HoldingsPolicy{},
	}})
	require.NoError(t, err)
	trans, err := illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{ID: uuid.NewString(), Timestamp: pgtype.Timestamp{Time: time.Now(), Valid: true}})
	require.NoError(t, err)
	symbol := "ISIL:" + uuid.NewString()
	_, err = illRepo.SaveSymbol(ctx, ill_db.SaveSymbolParams{SymbolValue: symbol, PeerID: peer.ID})
	require.NoError(t, err)
	supplier, err := illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{ID: uuid.NewString(), IllTransactionID: trans.ID, SupplierID: peer.ID, SupplierSymbol: symbol, SupplierStatus: ill_db.SupplierStateSelectedPg, LocalID: getPgText("bib"), ItemID: getPgText("previous-item"), HoldingsPolicyResolved: pgtype.Bool{Bool: true, Valid: true}})
	require.NoError(t, err)
	return supplier
}

func waitAvailabilityAttempt(t *testing.T, ctx context.Context, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal("availability attempt timed out")
	}
}

func TestAvailabilityAttemptOverlap(t *testing.T) {
	for _, newerFirst := range []bool{false, true} {
		for _, tc := range []struct{ name, oldResult, newResult string }{
			{"old success new failure", "old-item", "error"},
			{"old failure new success", "error", "new-item"},
			{"two successes", "old-item", "new-item"},
			{"old unavailable new success", "", "new-item"},
			{"old success new unavailable", "old-item", ""},
		} {
			order := "older finishes first"
			if newerFirst {
				order = "newer finishes first"
			}
			t.Run(tc.name+"/"+order, func(t *testing.T) {
				base, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ctx := common.CreateExtCtxWithArgs(base, nil)
				supplier := availabilityAttemptSelection(t, ctx)
				barriers := []*availabilityBarrier{newAvailabilityBarrier(), newAvailabilityBarrier()}
				defer barriers[0].resume()
				defer barriers[1].resume()
				buses := []*availabilityCaptureBus{new(availabilityCaptureBus), new(availabilityCaptureBus)}
				done := []chan struct{}{make(chan struct{}), make(chan struct{})}
				// Reusing the event ID also proves retries receive distinct attempt tokens.
				event := events.Event{ID: uuid.NewString(), IllTransactionID: supplier.IllTransactionID}
				var firstToken string
				for i, value := range []string{tc.oldResult, tc.newResult} {
					lookup := &pausedAvailabilityLookup{ctx: base, barrier: barriers[i]}
					if value == "error" {
						lookup.Err = errors.New("catalog failed")
					} else if value != "" {
						lookup.Holdings = []catalog.Holding{{ItemId: value}}
					}
					factory := service.NewLookupAdapterFactory(illRepo, nil, "", nil, availabilityCreator{lookup: lookup})
					locator := service.CreateSupplierLocator(buses[i], illRepo, nil, factory)
					go func() { defer close(done[i]); locator.CheckAvailability(ctx, event) }()
					waitAvailabilityAttempt(t, base, barriers[i].reached)
					attempt, err := illRepo.GetAvailabilityAttempt(ctx, supplier.IllTransactionID)
					require.NoError(t, err)
					if i == 0 {
						firstToken = attempt.Token
					} else {
						require.NotEqual(t, firstToken, attempt.Token)
					}
				}
				first := 0
				if newerFirst {
					first = 1
				}
				barriers[first].resume()
				waitAvailabilityAttempt(t, base, done[first])
				if !newerFirst {
					current, err := illRepo.GetSelectedSupplierForIllTransaction(ctx, supplier.IllTransactionID)
					require.NoError(t, err)
					require.False(t, current.HoldingsPolicyResolved.Bool, "older result must not restore readiness while newer lookup is pending")
					require.False(t, current.ItemID.Valid)
				}
				barriers[1-first].resume()
				waitAvailabilityAttempt(t, base, done[1-first])
				require.Equal(t, string(service.AvailabilitySuperseded), buses[0].completed.ResultData.CustomData[service.AvailabilityKey])
				workflow := service.CreateWorkflowManager(buses[0], illRepo, service.WorkflowConfig{})
				workflow.OnCheckAvailabilityComplete(ctx, buses[0].completed)
				require.Empty(t, buses[0].tasks)
				rows, _, err := illRepo.GetLocatedSuppliersByIllTransaction(ctx, supplier.IllTransactionID)
				require.NoError(t, err)
				require.Len(t, rows, 1)
				current := rows[0]
				ready := tc.newResult != "" && tc.newResult != "error"
				require.Equal(t, pgtype.Bool{Valid: true, Bool: ready}, current.HoldingsPolicyResolved)
				if ready {
					require.Equal(t, tc.newResult, current.ItemID.String)
				} else {
					require.False(t, current.ItemID.Valid)
				}
				require.Equal(t, tc.newResult == "", current.SupplierStatus == ill_db.SupplierStateSkippedPg)
				workflow = service.CreateWorkflowManager(buses[1], illRepo, service.WorkflowConfig{})
				workflow.OnCheckAvailabilityComplete(ctx, buses[1].completed)
				if tc.newResult == "error" {
					require.Equal(t, events.EventStatusError, buses[1].completed.EventStatus)
					require.Empty(t, buses[1].tasks)
				} else {
					require.Equal(t, events.EventStatusSuccess, buses[1].completed.EventStatus)
					require.NotEmpty(t, buses[1].tasks)
					require.Equal(t, buses[1].completed.ResultData.CustomData[ill_db.AvailabilityTokenKey], buses[1].taskData[0].CustomData[ill_db.AvailabilityTokenKey])
				}
			})
		}
	}
}

func TestAvailabilityAttemptGuardsDelayedWork(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	supplier := availabilityAttemptSelection(t, ctx)
	bus := new(availabilityCaptureBus)
	lookup := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{{ItemId: "first"}}}
	factory := service.NewLookupAdapterFactory(illRepo, nil, "", nil, availabilityCreator{lookup: lookup})
	locator := service.CreateSupplierLocator(bus, illRepo, nil, factory)
	event := events.Event{ID: uuid.NewString(), IllTransactionID: supplier.IllTransactionID}
	locator.CheckAvailability(ctx, event)
	completed := bus.completed
	workflow := service.CreateWorkflowManager(bus, illRepo, service.WorkflowConfig{})
	workflow.OnCheckAvailabilityComplete(ctx, completed)
	require.Len(t, bus.taskData, 2)
	oldData := bus.taskData[0]
	lookup.Err = errors.New("reevaluation failed")
	locator.CheckAvailability(ctx, event)
	require.Equal(t, events.EventStatusError, bus.completed.EventStatus)
	bus.tasks, bus.taskData = nil, nil
	workflow.OnCheckAvailabilityComplete(ctx, completed)
	require.Empty(t, bus.tasks, "delayed old completion must not create follow-up tasks")
	// A stale queued message must stop before trying to read the absent requester
	// or invoking HTTP/internal delivery. Both paths use the real client handler.
	isoClient := client.CreateIso18626Client(bus, illRepo, prservice.PatronRequestMessageHandler{}, 0, 0)
	event.EventData = oldData
	isoClient.MessageRequester(ctx, event)
	require.Equal(t, events.EventStatusSuccess, bus.completed.EventStatus)
	require.Equal(t, "superseded", bus.completed.ResultData.CustomData[service.AvailabilityKey])
	isoClient.MessageSupplier(ctx, event)
	require.Equal(t, events.EventStatusSuccess, bus.completed.EventStatus)
	require.Equal(t, "superseded", bus.completed.ResultData.CustomData[service.AvailabilityKey])
	// A delayed send failure must not schedule an unguarded reselection.
	event.EventStatus = events.EventStatusError
	workflow.OnMessageSupplierComplete(ctx, event)
	require.Empty(t, bus.tasks)
	// An old unavailable result cannot skip the current supplier either.
	event.EventData.CustomData[ill_db.AvailabilitySupplierStateKey] = ill_db.SupplierStateSkippedPg.String
	locator.SelectSupplier(ctx, event)
	require.Equal(t, "superseded", bus.completed.ResultData.CustomData[service.AvailabilityKey])
	workflow.OnSelectSupplierComplete(ctx, bus.completed)
	require.Empty(t, bus.tasks)
	current, err := illRepo.GetSelectedSupplierForIllTransaction(ctx, supplier.IllTransactionID)
	require.NoError(t, err)
	require.Equal(t, supplier.ID, current.ID)
	require.False(t, current.HoldingsPolicyResolved.Bool)
}

func TestAvailabilityAttemptCannotClearNewerSelection(t *testing.T) {
	base, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx := common.CreateExtCtxWithArgs(base, nil)
	supplier := availabilityAttemptSelection(t, ctx)
	barrier := newAvailabilityBarrier()
	defer barrier.resume()
	oldRepo := &pausedAvailabilityRepo{IllRepo: illRepo, barrier: barrier}
	oldBus, newBus := new(availabilityCaptureBus), new(availabilityCaptureBus)
	lookup := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{{ItemId: "new-item"}}}
	oldFactory := service.NewLookupAdapterFactory(oldRepo, nil, "", nil, availabilityCreator{lookup: lookup})
	oldLocator := service.CreateSupplierLocator(oldBus, oldRepo, nil, oldFactory)
	event := events.Event{ID: uuid.NewString(), IllTransactionID: supplier.IllTransactionID}
	done := make(chan struct{})
	go func() { defer close(done); oldLocator.CheckAvailability(ctx, event) }()
	waitAvailabilityAttempt(t, base, barrier.reached)
	factory := service.NewLookupAdapterFactory(illRepo, nil, "", nil, availabilityCreator{lookup: lookup})
	locator := service.CreateSupplierLocator(newBus, illRepo, nil, factory)
	locator.CheckAvailability(ctx, event)
	require.Equal(t, events.EventStatusSuccess, newBus.completed.EventStatus)
	barrier.resume()
	waitAvailabilityAttempt(t, base, done)
	require.Equal(t, string(service.AvailabilitySuperseded), oldBus.completed.ResultData.CustomData[service.AvailabilityKey])
	current, err := illRepo.GetSelectedSupplierForIllTransaction(ctx, supplier.IllTransactionID)
	require.NoError(t, err)
	require.Equal(t, "new-item", current.ItemID.String)
	require.True(t, current.HoldingsPolicyResolved.Bool)
}

func TestAvailabilityReselectionInvalidatesAttempt(t *testing.T) {
	for _, manuallySelected := range []bool{false, true} {
		t.Run(map[bool]string{false: "consume once", true: "manual selection supersedes unavailable"}[manuallySelected], func(t *testing.T) {
			ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
			supplier := availabilityAttemptSelection(t, ctx)
			bus := new(availabilityCaptureBus)
			factory := service.NewLookupAdapterFactory(illRepo, nil, "", nil, availabilityCreator{lookup: &catalog.MockLookupAdapter{}})
			locator := service.CreateSupplierLocator(bus, illRepo, nil, factory)
			event := events.Event{ID: uuid.NewString(), IllTransactionID: supplier.IllTransactionID}
			locator.CheckAvailability(ctx, event)
			workflow := service.CreateWorkflowManager(bus, illRepo, service.WorkflowConfig{})
			workflow.OnCheckAvailabilityComplete(ctx, bus.completed)
			require.Equal(t, []events.EventName{events.EventNameSelectSupplier}, bus.tasks)
			event.EventData = bus.taskData[0]
			state := ill_db.SupplierStateNewPg
			if manuallySelected {
				state = ill_db.SupplierStateSelectedPg
			}
			next, err := illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{ID: uuid.NewString(), IllTransactionID: supplier.IllTransactionID, SupplierID: supplier.SupplierID, SupplierSymbol: supplier.SupplierSymbol, SupplierStatus: state, Ordinal: 1, LocalID: getPgText("next-bib")})
			require.NoError(t, err)
			locator.SelectSupplier(ctx, event)
			require.Equal(t, events.EventStatusSuccess, bus.completed.EventStatus)
			if manuallySelected {
				require.Equal(t, "superseded", bus.completed.ResultData.CustomData[service.AvailabilityKey])
			} else {
				_, err = illRepo.GetAvailabilityAttempt(ctx, supplier.IllTransactionID)
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
			locator.SelectSupplier(ctx, event)
			require.Equal(t, "superseded", bus.completed.ResultData.CustomData[service.AvailabilityKey])
			selected, err := illRepo.GetSelectedSupplierForIllTransaction(ctx, supplier.IllTransactionID)
			require.NoError(t, err)
			require.Equal(t, next.ID, selected.ID)
		})
	}
}

func TestAvailabilityExhaustionCanNotifyRequester(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	supplier := availabilityAttemptSelection(t, ctx)
	bus := new(availabilityCaptureBus)
	factory := service.NewLookupAdapterFactory(illRepo, nil, "", nil, availabilityCreator{lookup: &catalog.MockLookupAdapter{}})
	locator := service.CreateSupplierLocator(bus, illRepo, nil, factory)
	event := events.Event{ID: uuid.NewString(), IllTransactionID: supplier.IllTransactionID}
	locator.CheckAvailability(ctx, event)
	workflow := service.CreateWorkflowManager(bus, illRepo, service.WorkflowConfig{})
	workflow.OnCheckAvailabilityComplete(ctx, bus.completed)
	event.EventData = bus.taskData[0]
	locator.SelectSupplier(ctx, event)
	require.NotEqual(t, events.EventStatusSuccess, bus.completed.EventStatus)
	workflow.OnSelectSupplierComplete(ctx, bus.completed)
	require.Equal(t, events.EventNameMessageRequester, bus.tasks[len(bus.tasks)-1])
	current, err := ill_db.CheckAvailabilityCurrent(ctx, illRepo, supplier.IllTransactionID, bus.taskData[len(bus.taskData)-1].CustomData)
	require.NoError(t, err)
	require.True(t, current, "exhausted rota must still notify the requester")
}

// A central-policy decision must survive even if an attempt stops after claiming
// its token, since no supplier endpoint exists to recompute that decision.
func TestCentralPolicyAvailabilityAttemptPreservesDiscovery(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		for _, readiness := range []pgtype.Bool{{Bool: true, Valid: true}, {Valid: true}, {}} {
			name := fmt.Sprintf("interrupted=%t/resolved=%t/known=%t", interrupted, readiness.Bool, readiness.Valid)
			t.Run(name, func(t *testing.T) {
				base, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				ctx := common.CreateExtCtxWithArgs(base, nil)
				supplier := availabilityAttemptSelection(t, ctx)
				peer, err := illRepo.GetPeerById(ctx, supplier.SupplierID)
				require.NoError(t, err)
				peer.CustomData.CatalogConfig = nil
				_, err = illRepo.SavePeer(ctx, ill_db.SavePeerParams(peer))
				require.NoError(t, err)
				supplier.HoldingsPolicyResolved = readiness
				_, err = illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(supplier))
				require.NoError(t, err)

				barrier := newAvailabilityBarrier()
				defer barrier.resume()
				oldRepo := &pausedAvailabilityRepo{IllRepo: illRepo, barrier: barrier}
				oldBus := new(availabilityCaptureBus)
				oldFactory := service.NewLookupAdapterFactory(oldRepo, nil, "", nil, availabilityCreator{})
				oldLocator := service.CreateSupplierLocator(oldBus, oldRepo, nil, oldFactory)
				event := events.Event{ID: uuid.NewString(), IllTransactionID: supplier.IllTransactionID}
				oldBase, stopOld := context.WithCancel(base)
				defer stopOld()
				done := make(chan struct{})
				go func() {
					defer close(done)
					oldLocator.CheckAvailability(common.CreateExtCtxWithArgs(oldBase, nil), event)
				}()
				waitAvailabilityAttempt(t, base, barrier.reached)
				first, err := illRepo.GetAvailabilityAttempt(ctx, supplier.IllTransactionID)
				require.NoError(t, err)
				pending, err := illRepo.GetSelectedSupplierForIllTransaction(ctx, supplier.IllTransactionID)
				require.NoError(t, err)
				require.Equal(t, readiness, pending.HoldingsPolicyResolved)
				require.Equal(t, supplier.ItemID, pending.ItemID)
				if interrupted {
					// Cancellation prevents the old attempt's deferred transaction.
					// A new service instance must recover using persisted state only.
					stopOld()
					waitAvailabilityAttempt(t, base, done)
				}

				bus := new(availabilityCaptureBus)
				factory := service.NewLookupAdapterFactory(illRepo, nil, "", nil, availabilityCreator{})
				locator := service.CreateSupplierLocator(bus, illRepo, nil, factory)
				locator.CheckAvailability(ctx, event)
				latest, err := illRepo.GetAvailabilityAttempt(ctx, supplier.IllTransactionID)
				require.NoError(t, err)
				require.NotEqual(t, first.Token, latest.Token)
				if readiness.Bool {
					require.Equal(t, events.EventStatusSuccess, bus.completed.EventStatus)
					require.Equal(t, string(service.AvailabilityUnknown), bus.completed.ResultData.CustomData[service.AvailabilityKey])
				} else {
					require.Equal(t, events.EventStatusError, bus.completed.EventStatus)
				}
				current, err := illRepo.GetSelectedSupplierForIllTransaction(ctx, supplier.IllTransactionID)
				require.NoError(t, err)
				require.Equal(t, readiness, current.HoldingsPolicyResolved)
				require.Equal(t, supplier.ItemID, current.ItemID)
				require.Equal(t, supplier.LocalID, current.LocalID)
				if !interrupted {
					barrier.resume()
					waitAvailabilityAttempt(t, base, done)
					require.Equal(t, string(service.AvailabilitySuperseded), oldBus.completed.ResultData.CustomData[service.AvailabilityKey])
					workflow := service.CreateWorkflowManager(oldBus, illRepo, service.WorkflowConfig{})
					workflow.OnCheckAvailabilityComplete(ctx, oldBus.completed)
					require.Empty(t, oldBus.tasks)
				}
			})
		}
	}
}
