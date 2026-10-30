package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/catalog"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/service"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

// The barrier pauses either after the initial supplier snapshot (before clearing)
// or inside Lookup (before the result is committed). No sleeps determine ordering.
type availabilityBarrier struct {
	reached, release chan struct{}
	once             sync.Once
}

func newAvailabilityBarrier() *availabilityBarrier {
	return &availabilityBarrier{reached: make(chan struct{}), release: make(chan struct{})}
}
func (b *availabilityBarrier) resume() { b.once.Do(func() { close(b.release) }) }
func (b *availabilityBarrier) pause(ctx context.Context) error {
	close(b.reached)
	select {
	case <-b.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type pausedAvailabilityRepo struct {
	ill_db.IllRepo
	barrier *availabilityBarrier
}

func (r *pausedAvailabilityRepo) GetPeerById(ctx common.ExtendedContext, id string) (ill_db.Peer, error) {
	if err := r.barrier.pause(ctx); err != nil {
		return ill_db.Peer{}, err
	}
	return r.IllRepo.GetPeerById(ctx, id)
}

type pausedAvailabilityLookup struct {
	catalog.MockLookupAdapter
	barrier *availabilityBarrier
	ctx     context.Context
}

func (l *pausedAvailabilityLookup) Lookup(params catalog.LookupParams) (catalog.LookupResult, error) {
	if l.barrier != nil {
		if err := l.barrier.pause(l.ctx); err != nil {
			return nil, err
		}
	}
	return l.MockLookupAdapter.Lookup(params)
}

type availabilityCreator struct{ lookup catalog.LookupAdapter }

func (c availabilityCreator) GetAdapter(common.ExtendedContext, ill_db.Peer) (catalog.LookupAdapter, error) {
	return c.lookup, nil
}

// Exercise the public locator handler directly; capture completion rather than
// publishing it to the application's unrelated background workflow consumers.
type availabilityCaptureBus struct {
	events.EventBus
	completed events.Event
	tasks     []events.EventName
	taskData  []events.EventData
}

func (b *availabilityCaptureBus) ProcessTask(ctx common.ExtendedContext, e events.Event, _ events.SignalTarget, h func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	status, result := h(ctx, e)
	e.EventStatus = status
	if result != nil {
		e.ResultData = *result
	}
	b.completed = e
	return e, nil
}
func (b *availabilityCaptureBus) CreateTask(_ string, name events.EventName, data events.EventData, _ events.EventDomain, _ *string, _ events.SignalTarget) (string, error) {
	b.tasks = append(b.tasks, name)
	b.taskData = append(b.taskData, data)
	return uuid.NewString(), nil
}

func TestAvailabilityConcurrentUpdates(t *testing.T) {
	for _, phase := range []string{"before clearing", "during lookup"} {
		for _, change := range []string{"retired", "deleted", "identity changed", "protocol response"} {
			t.Run(phase+"/"+change, func(t *testing.T) { testAvailabilityConcurrentUpdate(t, phase, change, false) })
		}
	}
	t.Run("lookup failure after retirement", func(t *testing.T) { testAvailabilityConcurrentUpdate(t, "during lookup", "retired", true) })
}

func testAvailabilityConcurrentUpdate(t *testing.T, phase, change string, lookupFails bool) {
	baseCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx := common.CreateExtCtxWithArgs(baseCtx, nil)
	peer, err := illRepo.SavePeer(ctx, ill_db.SavePeerParams{ID: uuid.NewString(), Name: "availability concurrency", Url: "https://supplier.invalid", RefreshPolicy: ill_db.RefreshPolicyNever, RefreshTime: pgtype.Timestamp{Time: time.Now(), Valid: true}, CustomData: dirapi.Entry{
		CatalogConfig: &dirapi.CatalogConfig{Sru: &dirapi.SruConfig{Address: "https://catalog.invalid/sru"}}, HoldingsPolicy: &dirapi.HoldingsPolicy{},
	}})
	require.NoError(t, err)
	trans, err := illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{ID: uuid.NewString(), Timestamp: pgtype.Timestamp{Time: time.Now(), Valid: true}})
	require.NoError(t, err)
	supplier, err := illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{ID: uuid.NewString(), IllTransactionID: trans.ID, SupplierID: peer.ID, SupplierSymbol: "ISIL:CONCURRENT", SupplierStatus: ill_db.SupplierStateSelectedPg, LocalID: getPgText("bib"), ItemID: getPgText("old-item")})
	require.NoError(t, err)
	barrier := newAvailabilityBarrier()
	defer barrier.resume()
	lookup := &pausedAvailabilityLookup{ctx: baseCtx, MockLookupAdapter: catalog.MockLookupAdapter{Holdings: []catalog.Holding{{ItemId: "new-item"}}}}
	if lookupFails {
		lookup.Err = errors.New("lookup failed")
	}
	repo := illRepo
	if phase == "before clearing" {
		repo = &pausedAvailabilityRepo{IllRepo: illRepo, barrier: barrier}
	} else {
		lookup.barrier = barrier
	}
	bus := new(availabilityCaptureBus)
	factory := service.NewLookupAdapterFactory(repo, nil, "", nil, availabilityCreator{lookup: lookup})
	locator := service.CreateSupplierLocator(bus, repo, nil, factory)
	done := make(chan struct{})
	go func() {
		defer close(done)
		locator.CheckAvailability(ctx, events.Event{ID: uuid.NewString(), IllTransactionID: trans.ID})
	}()
	select {
	case <-barrier.reached:
	case <-baseCtx.Done():
		t.Fatal("availability did not reach barrier")
	}
	// This must commit while lookup is paused, proving lookup holds no DB locks.
	err = illRepo.WithTxFunc(ctx, func(tx ill_db.IllRepo) error {
		if _, err := tx.GetIllTransactionByIdForUpdate(ctx, trans.ID); err != nil {
			return err
		}
		current, err := tx.GetLocatedSupplierByIdForUpdate(ctx, supplier.ID)
		if err != nil {
			return err
		}
		switch change {
		case "retired":
			return tx.SkipLocatedSuppliersByIllTransaction(ctx, trans.ID)
		case "deleted":
			return (&ill_db.Queries{}).DeleteLocatedSupplier(ctx, tx.(*ill_db.PgIllRepo).GetConnOrTx(), supplier.ID)
		case "identity changed":
			current.LocalID = getPgText("different-bib")
		case "protocol response":
			current.LastStatus = getPgText("WillSupply")
			current.PrevStatus = getPgText("ExpectToSupply")
			current.LastReason = getPgText("RequestResponse")
			current.SupplierRequestID = getPgText("supplier-request")
			current.Note = getPgText("new note")
		}
		_, err = tx.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(current))
		return err
	})
	barrier.resume()
	select {
	case <-done:
	case <-baseCtx.Done():
		t.Fatal("availability did not finish")
	}
	require.NoError(t, err)
	require.Equal(t, events.EventStatusSuccess, bus.completed.EventStatus)
	rows, _, err := illRepo.GetLocatedSuppliersByIllTransaction(ctx, trans.ID)
	require.NoError(t, err)
	if change == "deleted" {
		require.Empty(t, rows)
	} else {
		require.Len(t, rows, 1)
		current := rows[0]
		switch change {
		case "retired":
			require.Equal(t, ill_db.SupplierStateSkippedPg, current.SupplierStatus)
		case "identity changed":
			require.Equal(t, "different-bib", current.LocalID.String)
		case "protocol response":
			require.Equal(t, ill_db.SupplierStateSelectedPg, current.SupplierStatus)
			require.Equal(t, "new-item", current.ItemID.String)
			require.Equal(t, "WillSupply", current.LastStatus.String)
			require.Equal(t, "ExpectToSupply", current.PrevStatus.String)
			require.Equal(t, "RequestResponse", current.LastReason.String)
			require.Equal(t, "supplier-request", current.SupplierRequestID.String)
			require.Equal(t, "new note", current.Note.String)
		}
		if change != "protocol response" {
			if phase == "before clearing" {
				require.Equal(t, "old-item", current.ItemID.String)
			} else {
				require.False(t, current.ItemID.Valid)
			}
		}
	}
	if change != "protocol response" {
		require.Equal(t, string(service.AvailabilitySuperseded), bus.completed.ResultData.CustomData[service.AvailabilityKey])
		workflow := service.CreateWorkflowManager(bus, illRepo, service.WorkflowConfig{})
		workflow.OnCheckAvailabilityComplete(ctx, bus.completed)
		require.Empty(t, bus.tasks, "superseded lookup must not advance or notify the workflow")
	} else {
		require.Equal(t, string(service.AvailabilityAvailable), bus.completed.ResultData.CustomData[service.AvailabilityKey])
	}
}

// Readiness must survive repository round trips independently of ItemID. A
// failed reevaluation must also revoke a previously successful local selection.
func TestLocalPolicyReadinessRetry(t *testing.T) {
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	peer, err := illRepo.SavePeer(ctx, ill_db.SavePeerParams{ID: uuid.NewString(), Name: "local policy retry", Url: "https://supplier.invalid", RefreshPolicy: ill_db.RefreshPolicyNever, RefreshTime: pgtype.Timestamp{Time: time.Now(), Valid: true}, CustomData: dirapi.Entry{
		CatalogConfig: &dirapi.CatalogConfig{Sru: &dirapi.SruConfig{Address: "https://catalog.invalid/sru"}}, HoldingsPolicy: &dirapi.HoldingsPolicy{},
	}})
	require.NoError(t, err)
	trans, err := illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{ID: uuid.NewString(), Timestamp: pgtype.Timestamp{Time: time.Now(), Valid: true}})
	require.NoError(t, err)
	_, err = illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{ID: uuid.NewString(), IllTransactionID: trans.ID, SupplierID: peer.ID, SupplierSymbol: "ISIL:LOCAL", SupplierStatus: ill_db.SupplierStateSelectedPg, LocalID: getPgText("bib"), LocalSupplier: true})
	require.NoError(t, err)
	lookup := &catalog.MockLookupAdapter{Holdings: []catalog.Holding{{}}}
	bus := new(availabilityCaptureBus)
	factory := service.NewLookupAdapterFactory(illRepo, nil, "", nil, availabilityCreator{lookup: lookup})
	locator := service.CreateSupplierLocator(bus, illRepo, nil, factory)
	workflow := service.CreateWorkflowManager(bus, illRepo, service.WorkflowConfig{})
	for _, fails := range []bool{true, false, true} {
		bus.tasks = nil
		lookup.Err = nil
		if fails {
			lookup.Err = errors.New("catalog unavailable")
		}
		locator.CheckAvailability(ctx, events.Event{ID: uuid.NewString(), IllTransactionID: trans.ID})
		workflow.OnCheckAvailabilityComplete(ctx, bus.completed)
		rows, _, err := illRepo.GetLocatedSuppliersByIllTransaction(ctx, trans.ID)
		require.NoError(t, err)
		require.Len(t, rows, 1)
		selected := rows[0]
		require.Equal(t, pgtype.Bool{Bool: !fails, Valid: true}, selected.HoldingsPolicyResolved)
		require.False(t, selected.ItemID.Valid)
		require.Equal(t, "bib", selected.LocalID.String)
		if fails {
			require.Equal(t, events.EventStatusError, bus.completed.EventStatus)
			require.Empty(t, bus.tasks, "failed policy must not announce local supply")
			require.Error(t, selected.ValidateHoldingsPolicy(peer.CustomData))
		} else {
			require.Equal(t, events.EventStatusSuccess, bus.completed.EventStatus)
			require.NotEmpty(t, bus.tasks, "successful retry must resume workflow")
			require.NoError(t, selected.ValidateHoldingsPolicy(peer.CustomData))
		}
	}
}
