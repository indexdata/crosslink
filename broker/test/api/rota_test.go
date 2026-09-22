package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	brokerapi "github.com/indexdata/crosslink/broker/api"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	"github.com/indexdata/crosslink/broker/oapi"
	prdb "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/indexdata/crosslink/broker/service"
	"github.com/indexdata/crosslink/broker/tenant"
	apptest "github.com/indexdata/crosslink/broker/test/apputils"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type rotaDirectory struct {
	adapter.DirectoryLookupAdapter
	entries []adapter.DirectoryEntry
	err     error
}

func (d rotaDirectory) Lookup(ctx common.ExtendedContext, p adapter.DirectoryLookupParams) ([]adapter.DirectoryEntry, string, error) {
	var result []adapter.DirectoryEntry
	for _, e := range d.entries {
		for _, s := range e.Symbols {
			for _, want := range p.Symbols {
				if s == want {
					result = append(result, e)
				}
			}
		}
	}
	return result, "test", d.err
}

type rotaFixture struct {
	ctx       common.ExtendedContext
	id        string
	suppliers []ill_db.LocatedSupplier
	handler   http.Handler
	directory rotaDirectory
	newSymbol string
}

func newRotaFixture(t *testing.T, statuses ...string) rotaFixture {
	t.Helper()
	ctx := common.CreateExtCtxWithArgs(context.Background(), nil)
	requester, err := illRepo.GetPeerBySymbol(ctx, "ISIL:DK-DIKU")
	if err != nil {
		requester = apptest.CreatePeer(t, illRepo, "ISIL:DK-DIKU", "https://requester.example/iso18626")
	}
	if _, err := illRepo.GetPeerBySymbol(ctx, "ISIL:DK-RUC"); err != nil {
		apptest.CreatePeer(t, illRepo, "ISIL:DK-RUC", "https://other.example/iso18626")
	}
	trans, err := illRepo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{ID: uuid.NewString(), Timestamp: apptest.GetNow(), RequesterID: pgtype.Text{String: requester.ID, Valid: true}, RequesterSymbol: pgtype.Text{String: "ISIL:DK-DIKU", Valid: true}})
	require.NoError(t, err)
	f := rotaFixture{ctx: ctx, id: trans.ID, newSymbol: "ISIL:NEW-" + uuid.NewString()}
	for i, status := range statuses {
		symbol := "ISIL:ROTA-" + uuid.NewString()
		peer := apptest.CreatePeer(t, illRepo, symbol, "https://supplier.example/iso18626")
		supplier, err := illRepo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{ID: uuid.NewString(), IllTransactionID: f.id, SupplierID: peer.ID, SupplierSymbol: symbol, Ordinal: int32(i * 2), SupplierStatus: pgtype.Text{String: status, Valid: true}})
		require.NoError(t, err)
		f.suppliers = append(f.suppliers, supplier)
	}
	f.directory = rotaDirectory{entries: []adapter.DirectoryEntry{{Symbols: []string{f.newSymbol}, Name: "Manual supplier", URL: "https://manual.example/iso18626"}}}
	f.handler = rotaHandler(illRepo, f.directory)
	return f
}
func rotaHandler(repo ill_db.IllRepo, directory rotaDirectory) http.Handler {
	resolver := tenant.NewResolver().WithIllRepo(repo).WithTenantToSymbol("ISIL:DK-{tenant}").WithLookupAdapter(directory)
	handler := brokerapi.NewApiHandler(eventRepo, repo, resolver, directory, 10)
	return oapi.HandlerFromMuxWithBaseURL(&handler, http.NewServeMux(), "/broker")
}
func (f rotaFixture) post(path, body, tenantName string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/broker/ill_transactions/"+f.id+"/located_suppliers"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(tenant.OkapiTenantHeader, tenantName)
	r.Header.Set(tenant.OkapiUserHeader, "staff-user")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func (f rotaFixture) move(index int, offset int64) *httptest.ResponseRecorder {
	return f.post("/"+f.suppliers[index].ID+"/move", fmt.Sprintf(`{"offset":%d}`, offset), "diku")
}
func (f rotaFixture) add(symbol string) *httptest.ResponseRecorder {
	return f.post("", fmt.Sprintf(`{"supplierSymbol":%q,"localId":"record-123"}`, symbol), "diku")
}
func (f rotaFixture) rows(t *testing.T) []ill_db.LocatedSupplier {
	t.Helper()
	rows, _, err := illRepo.GetLocatedSuppliersByIllTransaction(f.ctx, f.id)
	require.NoError(t, err)
	return rows
}

func TestManualRotaMove(t *testing.T) {
	f := newRotaFixture(t, "skipped", "new", "selected", "new", "new")
	w := f.move(4, math.MinInt64)
	require.Equal(t, 200, w.Code, w.Body.String())
	rows := f.rows(t)
	require.Equal(t, []string{f.suppliers[0].ID, f.suppliers[4].ID, f.suppliers[2].ID, f.suppliers[1].ID, f.suppliers[3].ID}, rotaIDs(rows))
	require.Equal(t, f.suppliers[0], rows[0])
	require.Equal(t, f.suppliers[2], rows[2])
	var resp oapi.LocatedSuppliers
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 5)
	audit, _, err := eventRepo.GetIllTransactionEvents(f.ctx, f.id)
	require.NoError(t, err)
	require.Len(t, audit, 1)
	require.Equal(t, events.EventNameSupplierMoved, audit[0].EventName)
	require.Equal(t, "staff-user", audit[0].EventData.User)
	require.Equal(t, 200, f.move(4, -1).Code)
	require.Equal(t, 200, f.move(4, 0).Code)
	audit, _, err = eventRepo.GetIllTransactionEvents(f.ctx, f.id)
	require.NoError(t, err)
	require.Len(t, audit, 1)
	require.Equal(t, 200, f.move(4, math.MaxInt64).Code)
	require.Equal(t, rotaIDs(f.suppliers), rotaIDs(f.rows(t)))
	require.Equal(t, 200, f.move(4, 1).Code)
	require.Equal(t, 409, f.move(0, 1).Code)
	require.Equal(t, 409, f.move(2, 1).Code)
	require.Equal(t, 404, f.post("/missing/move", `{"offset":1}`, "diku").Code)
	other := newRotaFixture(t, "new")
	require.Equal(t, 404, f.post("/"+other.suppliers[0].ID+"/move", `{"offset":1}`, "diku").Code)
}
func rotaIDs(rows []ill_db.LocatedSupplier) []string {
	var ids []string
	for _, s := range rows {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestManualRotaAdd(t *testing.T) {
	for _, statuses := range [][]string{nil, {"selected", "skipped"}, {"skipped", "new", "selected", "new"}} {
		t.Run(fmt.Sprint(statuses), func(t *testing.T) {
			f := newRotaFixture(t, statuses...)
			w := f.add(f.newSymbol)
			require.Equal(t, 201, w.Code, w.Body.String())
			var added oapi.LocatedSupplier
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
			require.Equal(t, "record-123", *added.LocalID)
			require.Equal(t, oapi.LocatedSupplierStatus("new"), *added.SupplierStatus)
			rows := f.rows(t)
			require.Len(t, rows, len(statuses)+1)
			var newIDs []string
			for _, row := range rows {
				if row.SupplierStatus == ill_db.SupplierStateNewPg {
					newIDs = append(newIDs, row.ID)
				}
			}
			require.Equal(t, added.Id, newIDs[0])
			for _, before := range f.suppliers {
				if before.SupplierStatus != ill_db.SupplierStateNewPg {
					for _, row := range rows {
						if row.ID == before.ID {
							require.Equal(t, before, row)
						}
					}
				}
			}
			require.Equal(t, 409, f.add(f.newSymbol).Code)
			for _, supplier := range f.suppliers {
				require.Equal(t, 409, f.add(supplier.SupplierSymbol).Code)
			}
			audit, _, err := eventRepo.GetIllTransactionEvents(f.ctx, f.id)
			require.NoError(t, err)
			require.Len(t, audit, 1)
			require.Equal(t, events.EventNameSupplierAdded, audit[0].EventName)
		})
	}
}

func TestManualRotaValidationAndPermissions(t *testing.T) {
	f := newRotaFixture(t, "new")
	path := "/" + f.suppliers[0].ID + "/move"
	for _, body := range []string{`{}`, `null`, `{"offset":null}`, `{"offset":1.5}`, `{"offset":9223372036854775808}`, `{"offset":1,"extra":true}`, `{"offset":1} {}`, strings.Repeat(" ", 4097) + `{"offset":1}`} {
		require.Equal(t, 400, f.post(path, body, "diku").Code, body)
	}
	for _, body := range []string{`{}`, `{"supplierSymbol":"ISIL:X"}`, `{"supplierSymbol":"X","localId":"1"}`, `{"supplierSymbol":"ISIL:X","localId":" "}`} {
		require.Equal(t, 400, f.post("", body, "diku").Code, body)
	}
	require.Equal(t, 400, f.post(path, `{"offset":1}`, "").Code)
	require.Equal(t, 404, f.post(path, `{"offset":1}`, "ruc").Code)
	require.Equal(t, 404, f.post("", fmt.Sprintf(`{"supplierSymbol":%q,"localId":"1"}`, f.newSymbol), "ruc").Code)
	require.Equal(t, 422, f.add("ISIL:UNKNOWN").Code)
	f.directory.entries[0].URL = ""
	f.handler = rotaHandler(illRepo, f.directory)
	require.Equal(t, 422, f.add(f.newSymbol).Code)
	f.directory.err = errors.New("directory unavailable")
	f.handler = rotaHandler(illRepo, f.directory)
	require.Equal(t, 500, f.add(f.newSymbol).Code)
	for _, status := range []string{"LoanCompleted", "CopyCompleted", "CompletedWithoutReturn"} {
		trans, err := illRepo.GetIllTransactionById(f.ctx, f.id)
		require.NoError(t, err)
		trans.LastSupplierStatus = pgtype.Text{String: status, Valid: true}
		_, err = illRepo.SaveIllTransaction(f.ctx, ill_db.SaveIllTransactionParams(trans))
		require.NoError(t, err)
		require.Equal(t, 409, f.move(0, 1).Code)
		require.Equal(t, 409, f.add(f.newSymbol).Code)
	}
	f.id = uuid.NewString()
	require.Equal(t, 404, f.move(0, 1).Code)
	require.Equal(t, 404, f.add(f.newSymbol).Code)
}

// Force an audit failure after ordinal updates to verify they roll back together.
type failingRotaAudit struct{ ill_db.IllRepo }

func (r failingRotaAudit) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return r.IllRepo.WithTxFunc(ctx, func(tx ill_db.IllRepo) error { return fn(failingRotaAudit{tx}) })
}
func (r failingRotaAudit) SaveRotaAudit(ctx common.ExtendedContext, id, name, user string, data map[string]any) error {
	return errors.New("audit failure")
}
func TestManualRotaAuditRollback(t *testing.T) {
	f := newRotaFixture(t, "new", "new")
	f.handler = rotaHandler(failingRotaAudit{illRepo}, f.directory)
	require.Equal(t, 500, f.move(1, -1).Code)
	require.Equal(t, f.suppliers, f.rows(t))
	require.Equal(t, 500, f.add(f.newSymbol).Code)
	require.Equal(t, f.suppliers, f.rows(t))
	audit, _, err := eventRepo.GetIllTransactionEvents(f.ctx, f.id)
	require.NoError(t, err)
	require.Empty(t, audit)
}

// Execute the real selection callback synchronously, without scheduling downstream work.
type rotaSelectionBus struct {
	events.EventBus
	status events.EventStatus
}

func (b *rotaSelectionBus) ProcessTask(ctx common.ExtendedContext, e events.Event, target events.SignalTarget, fn func(common.ExtendedContext, events.Event) (events.EventStatus, *events.EventResult)) (events.Event, error) {
	b.status, _ = fn(ctx, e)
	return e, nil
}

type pausedRotaRepo struct {
	ill_db.IllRepo
	locked, release chan struct{}
}

func (r pausedRotaRepo) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return r.IllRepo.WithTxFunc(ctx, func(tx ill_db.IllRepo) error { return fn(pausedRotaRepo{tx, r.locked, r.release}) })
}
func (r pausedRotaRepo) GetIllTransactionByIdForUpdate(ctx common.ExtendedContext, id string) (ill_db.IllTransaction, error) {
	trans, err := r.IllRepo.GetIllTransactionByIdForUpdate(ctx, id)
	if err == nil {
		close(r.locked)
		select {
		case <-r.release:
		case <-ctx.Done():
			return trans, ctx.Err()
		}
	}
	return trans, err
}
func awaitRota(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for rota operation")
	}
}
func TestManualRotaConcurrentSelection(t *testing.T) {
	for _, first := range []string{"selection", "move", "add"} {
		t.Run(first, func(t *testing.T) {
			f := newRotaFixture(t, "new", "new")
			blocked := pausedRotaRepo{illRepo, make(chan struct{}), make(chan struct{})}
			bus := &rotaSelectionBus{}
			selectionRepo := illRepo
			if first == "selection" {
				selectionRepo = blocked
			} else {
				f.handler = rotaHandler(blocked, f.directory)
			}
			locator := service.CreateSupplierLocator(bus, selectionRepo, f.directory, nil)
			selected, edited := make(chan struct{}), make(chan struct{})
			var response *httptest.ResponseRecorder
			selectSupplier := func() { locator.SelectSupplier(f.ctx, events.Event{IllTransactionID: f.id}); close(selected) }
			edit := func() {
				if first == "add" {
					response = f.add(f.newSymbol)
				} else {
					response = f.move(0, 1)
				}
				close(edited)
			}
			if first == "selection" {
				go selectSupplier()
				awaitRota(t, blocked.locked)
				go edit()
			} else {
				go edit()
				awaitRota(t, blocked.locked)
				go selectSupplier()
			}
			close(blocked.release)
			awaitRota(t, selected)
			awaitRota(t, edited)
			require.Equal(t, events.EventStatusSuccess, bus.status)
			current, err := illRepo.GetSelectedSupplierForIllTransaction(f.ctx, f.id)
			require.NoError(t, err)
			switch first {
			case "selection":
				require.Equal(t, 409, response.Code, response.Body.String())
				require.Equal(t, f.suppliers[0].ID, current.ID)
			case "move":
				require.Equal(t, 200, response.Code, response.Body.String())
				require.Equal(t, f.suppliers[1].ID, current.ID)
			case "add":
				require.Equal(t, 201, response.Code, response.Body.String())
				require.Equal(t, f.newSymbol, current.SupplierSymbol)
			}
			rows := f.rows(t)
			seen := map[int32]bool{}
			for _, row := range rows {
				require.False(t, seen[row.Ordinal])
				seen[row.Ordinal] = true
			}
		})
	}
}

func TestManualRotaConcurrentDuplicate(t *testing.T) {
	f := newRotaFixture(t, "new")
	blocked := pausedRotaRepo{illRepo, make(chan struct{}), make(chan struct{})}
	first := f
	first.handler = rotaHandler(blocked, f.directory)
	firstDone, secondDone := make(chan struct{}), make(chan struct{})
	var firstResponse, secondResponse *httptest.ResponseRecorder
	go func() { firstResponse = first.add(f.newSymbol); close(firstDone) }()
	awaitRota(t, blocked.locked)
	go func() { secondResponse = f.add(f.newSymbol); close(secondDone) }()
	close(blocked.release)
	awaitRota(t, firstDone)
	awaitRota(t, secondDone)
	require.Equal(t, 201, firstResponse.Code, firstResponse.Body.String())
	require.Equal(t, 409, secondResponse.Code, secondResponse.Body.String())
	require.Len(t, f.rows(t), 2)
}

func TestManualRotaAuditNotification(t *testing.T) {
	f := newRotaFixture(t, "new", "new")
	conn, err := illRepo.(*ill_db.PgIllRepo).Pool.Acquire(f.ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(f.ctx, "LISTEN crosslink_channel")
	require.NoError(t, err)
	defer func() { _, err := conn.Exec(f.ctx, "UNLISTEN crosslink_channel"); require.NoError(t, err) }()
	require.Equal(t, 200, f.move(1, -1).Code)
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	notification, err := conn.Conn().WaitForNotification(ctx)
	require.NoError(t, err)
	var notice events.NotifyData
	require.NoError(t, json.Unmarshal([]byte(notification.Payload), &notice))
	require.Equal(t, events.SignalNoticeCreated, notice.Signal)
	require.Equal(t, events.SignalObservers, notice.Target)
	event, err := eventRepo.GetEvent(f.ctx, notice.Event)
	require.NoError(t, err)
	require.Equal(t, f.id, event.IllTransactionID)
	require.Equal(t, events.EventNameSupplierMoved, event.EventName)
	require.Equal(t, f.suppliers[1].ID, f.rows(t)[0].ID)
}

func TestManualRotaArchived(t *testing.T) {
	f := newRotaFixture(t, "new")

	// Create this archived fixture independently of the shared archiver's
	// session advisory lock, which other tests in this suite also exercise.
	require.NoError(t, illRepo.WithTxFunc(f.ctx, func(repo ill_db.IllRepo) error {
		_, err := repo.(*ill_db.PgIllRepo).GetConnOrTx().Exec(f.ctx,
			"INSERT INTO archived_ill_transactions (ill_transaction, events, located_suppliers) SELECT to_jsonb(t), '[]'::jsonb, '[]'::jsonb FROM ill_transaction t WHERE id=$1", f.id)
		if err != nil {
			return err
		}
		return repo.DeleteIllTransaction(f.ctx, f.id)
	}))

	require.Equal(t, 404, f.move(0, 0).Code)
	require.Equal(t, 404, f.add(f.newSymbol).Code)
}

func (f rotaFixture) borrowingRequest(t *testing.T) (prdb.PrRepo, prdb.PatronRequest) {
	t.Helper()
	trans, err := illRepo.GetIllTransactionById(f.ctx, f.id)
	require.NoError(t, err)
	trans.RequesterRequestID = pgtype.Text{String: uuid.NewString(), Valid: true}
	_, err = illRepo.SaveIllTransaction(f.ctx, ill_db.SaveIllTransactionParams(trans))
	require.NoError(t, err)
	repo := prdb.CreatePrRepo(illRepo.(*ill_db.PgIllRepo).Pool, false)
	pr, err := repo.CreatePatronRequest(f.ctx, prdb.CreatePatronRequestParams{ID: uuid.NewString(), State: "SENT", StateModel: "default", Items: []prdb.PrItem{}, Language: "english", Side: prdb.PatronRequestSide("borrowing"), RequesterSymbol: trans.RequesterSymbol, RequesterReqID: trans.RequesterRequestID, CreatedAt: apptest.GetNow(), UpdatedAt: apptest.GetNow()})
	require.NoError(t, err)
	return repo, pr
}

func TestManualRotaTerminalBorrowingRequest(t *testing.T) {
	f := newRotaFixture(t, "new", "new")
	repo, pr := f.borrowingRequest(t)
	trans, err := illRepo.GetIllTransactionById(f.ctx, f.id)
	require.NoError(t, err)
	// Cancelling one supplier (for example after rejecting a condition) does
	// not complete the borrowing request. Remaining new suppliers stay editable.
	trans.LastSupplierStatus = pgtype.Text{String: "Cancelled", Valid: true}
	_, err = illRepo.SaveIllTransaction(f.ctx, ill_db.SaveIllTransactionParams(trans))
	require.NoError(t, err)
	// Having a linked patron request does not disable edits while it is active.
	require.Equal(t, 200, f.move(0, 1).Code)
	pr.TerminalState = true
	_, err = repo.UpdatePatronRequest(f.ctx, prdb.UpdatePatronRequestParams(pr))
	require.NoError(t, err)
	require.Equal(t, 409, f.move(0, 0).Code)
	require.Equal(t, 409, f.add(f.newSymbol).Code)
}

func TestManualRotaAdditionStillChecksClosures(t *testing.T) {
	f := newRotaFixture(t, "new")
	require.Equal(t, 201, f.add(f.newSymbol).Code)
	peer, err := illRepo.GetPeerBySymbol(f.ctx, f.newSymbol)
	require.NoError(t, err)
	// Close the manually added supplier around today's date.
	data := fmt.Sprintf(`{"closures":[{"startDate":%q,"endDate":%q}],"timeZone":"UTC"}`, time.Now().Add(-48*time.Hour).Format("2006-01-02"), time.Now().Add(48*time.Hour).Format("2006-01-02"))
	require.NoError(t, json.Unmarshal([]byte(data), &peer.CustomData))
	_, err = illRepo.SavePeer(f.ctx, ill_db.SavePeerParams(peer))
	require.NoError(t, err)
	bus := &rotaSelectionBus{}
	locator := service.CreateSupplierLocator(bus, illRepo, f.directory, nil)
	locator.SelectSupplier(f.ctx, events.Event{IllTransactionID: f.id})
	require.Equal(t, events.EventStatusSuccess, bus.status)
	selected, err := illRepo.GetSelectedSupplierForIllTransaction(f.ctx, f.id)
	require.NoError(t, err)
	require.Equal(t, f.suppliers[0].ID, selected.ID)
	manual, err := illRepo.GetLocatedSupplierByIllTransactionAndSymbol(f.ctx, f.id, f.newSymbol)
	require.NoError(t, err)
	require.Equal(t, ill_db.SupplierStateSkippedPg, manual.SupplierStatus)
}

func TestManualRotaRequesterTenants(t *testing.T) {
	for _, tenantName := range []string{"diku", "ruc"} {
		t.Run(tenantName, func(t *testing.T) {
			f := newRotaFixture(t, "new", "new")
			trans, err := illRepo.GetIllTransactionById(f.ctx, f.id)
			require.NoError(t, err)
			symbol := "ISIL:DK-" + strings.ToUpper(tenantName)
			peer, err := illRepo.GetPeerBySymbol(f.ctx, symbol)
			require.NoError(t, err)
			trans.RequesterID = pgtype.Text{String: peer.ID, Valid: true}
			trans.RequesterSymbol = pgtype.Text{String: symbol, Valid: true}
			_, err = illRepo.SaveIllTransaction(f.ctx, ill_db.SaveIllTransactionParams(trans))
			require.NoError(t, err)
			path := "/" + f.suppliers[1].ID + "/move"
			require.Equal(t, http.StatusOK, f.post(path, `{"offset":-1}`, tenantName).Code)
			body := fmt.Sprintf(`{"supplierSymbol":%q,"localId":"record-123"}`, f.newSymbol)
			require.Equal(t, http.StatusCreated, f.post("", body, tenantName).Code)
			other := "ruc"
			if tenantName == other {
				other = "diku"
			}
			require.Equal(t, http.StatusNotFound, f.post(path, `{"offset":1}`, other).Code)
			require.Equal(t, http.StatusNotFound, f.post("", body, other).Code)
			for _, missing := range []string{"", "  "} {
				require.Equal(t, http.StatusBadRequest, f.post(path, `{"offset":1}`, missing).Code)
				require.Equal(t, http.StatusBadRequest, f.post("", body, missing).Code)
			}
		})
	}
}

// Pause only the transactional check: the request's preflight has already run.
// This exposes the exact interval where a concurrent completion used to slip in.
type rotaClosureBarrier struct {
	ill_db.IllRepo
	inTx, beforeCheck bool
	paused            chan int
	resume            <-chan struct{}
}

func (r rotaClosureBarrier) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return r.IllRepo.WithTxFunc(ctx, func(tx ill_db.IllRepo) error {
		r.IllRepo = tx
		r.inTx = true
		return fn(r)
	})
}

func (r rotaClosureBarrier) pause(ctx common.ExtendedContext) error {
	r.paused <- int(r.IllRepo.(*ill_db.PgIllRepo).Tx.Conn().PgConn().PID())
	select {
	case <-r.resume:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r rotaClosureBarrier) RotaRequestClosed(ctx common.ExtendedContext, id string) (bool, error) {
	if r.inTx && r.beforeCheck {
		if err := r.pause(ctx); err != nil {
			return false, err
		}
	}
	closed, err := r.IllRepo.RotaRequestClosed(ctx, id)
	if err == nil && r.inTx && !r.beforeCheck {
		err = r.pause(ctx)
	}
	return closed, err
}

func awaitRotaPID(t *testing.T, ctx context.Context, pids <-chan int) int {
	t.Helper()
	select {
	case pid := <-pids:
		return pid
	case <-ctx.Done():
		t.Fatal(ctx.Err())
		return 0
	}
}

func requireRotaBlockedBy(t *testing.T, ctx context.Context, blocked, blocker int) {
	t.Helper()
	// Observe the actual database wait, not an assumption based on a sleep.
	require.Eventually(t, func() bool {
		var waiting bool
		err := illRepo.(*ill_db.PgIllRepo).Pool.QueryRow(ctx, "SELECT $1::int = ANY(pg_blocking_pids($2::int))", blocker, blocked).Scan(&waiting)
		return err == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "backend %d should wait for backend %d", blocked, blocker)
}

func TestManualRotaConcurrentCompletion(t *testing.T) {
	for _, operation := range []string{"move", "add"} {
		for _, first := range []string{"completion", "edit"} {
			t.Run(operation+"/"+first, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				f := newRotaFixture(t, "new", "new")
				f.ctx = common.CreateExtCtxWithArgs(ctx, nil)
				prRepo, pr := f.borrowingRequest(t)
				editResume, completionResume := make(chan struct{}), make(chan struct{})
				releaseEdit := sync.OnceFunc(func() { close(editResume) })
				releaseCompletion := sync.OnceFunc(func() { close(completionResume) })
				defer releaseEdit()
				defer releaseCompletion()
				barrier := rotaClosureBarrier{IllRepo: illRepo, beforeCheck: first == "completion", paused: make(chan int, 1), resume: editResume}
				handler := rotaHandler(barrier, f.directory)
				f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r.WithContext(ctx)) })
				editDone := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					if operation == "move" {
						editDone <- f.move(1, -1)
					} else {
						editDone <- f.add(f.newSymbol)
					}
				}()
				editPID := awaitRotaPID(t, ctx, barrier.paused)
				completionPIDs := make(chan int, 1)
				completionDone := make(chan error, 1)
				go func() {
					completionDone <- prRepo.WithTxFunc(f.ctx, func(repo prdb.PrRepo) error {
						pid := int(repo.(*prdb.PgPrRepo).Tx.Conn().PgConn().PID())
						if first == "edit" {
							completionPIDs <- pid
						}
						pr.TerminalState = true
						if _, err := repo.UpdatePatronRequest(f.ctx, prdb.UpdatePatronRequestParams(pr)); err != nil {
							return err
						}
						if first == "completion" {
							completionPIDs <- pid
							select {
							case <-completionResume:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						// Import updates use patron-request -> ILL order. Acquiring the ILL lock
						// here proves the rota does not hold it while waiting for the patron row.
						_, err := (&ill_db.Queries{}).GetIllTransactionByIdForUpdate(f.ctx, repo.(*prdb.PgPrRepo).GetConnOrTx(), f.id)
						return err
					})
				}()
				completionPID := awaitRotaPID(t, ctx, completionPIDs)
				if first == "completion" {
					releaseEdit()
					requireRotaBlockedBy(t, ctx, editPID, completionPID)
					releaseCompletion()
				} else {
					requireRotaBlockedBy(t, ctx, completionPID, editPID)
					releaseEdit()
				}
				var response *httptest.ResponseRecorder
				select {
				case response = <-editDone:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case err := <-completionDone:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				audit, _, err := eventRepo.GetIllTransactionEvents(f.ctx, f.id)
				require.NoError(t, err)
				if first == "completion" {
					require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
					require.Equal(t, f.suppliers, f.rows(t))
					require.Empty(t, audit)
				} else {
					status := http.StatusOK
					if operation == "add" {
						status = http.StatusCreated
					}
					require.Equal(t, status, response.Code, response.Body.String())
					require.Len(t, audit, 1)
				}
				closed, err := prRepo.GetPatronRequestById(f.ctx, pr.ID)
				require.NoError(t, err)
				require.True(t, closed.TerminalState)
				require.Equal(t, http.StatusConflict, f.move(0, 0).Code)
			})
		}
	}
}

func TestManualRotaRequesterChangesWhileLocking(t *testing.T) {
	for _, field := range []string{"request ID", "requester symbol"} {
		t.Run(field, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			f := newRotaFixture(t, "new", "new")
			f.ctx = common.CreateExtCtxWithArgs(ctx, nil)
			f.borrowingRequest(t)
			resume := make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			defer release()
			barrier := rotaClosureBarrier{IllRepo: illRepo, paused: make(chan int, 1), resume: resume}
			handler := rotaHandler(barrier, f.directory)
			f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r.WithContext(ctx)) })
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- f.move(1, -1) }()
			awaitRotaPID(t, ctx, barrier.paused)
			trans, err := illRepo.GetIllTransactionById(f.ctx, f.id)
			require.NoError(t, err)
			if field == "request ID" {
				trans.RequesterRequestID.String = uuid.NewString()
			} else {
				trans.RequesterSymbol.String = "ISIL:DK-RUC"
			}
			_, err = illRepo.SaveIllTransaction(f.ctx, ill_db.SaveIllTransactionParams(trans))
			require.NoError(t, err)
			release()
			select {
			case response := <-done:
				require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			require.Equal(t, f.suppliers, f.rows(t))
			audit, _, err := eventRepo.GetIllTransactionEvents(f.ctx, f.id)
			require.NoError(t, err)
			require.Empty(t, audit)
		})
	}
}

// Pause immediately before or after the supplier snapshot, while retaining the
// transaction's locks. Protocol updates take only the supplier lock.
type rotaSupplierBarrier struct {
	rotaClosureBarrier
}

func (r rotaSupplierBarrier) WithTxFunc(ctx common.ExtendedContext, fn func(ill_db.IllRepo) error) error {
	return r.IllRepo.WithTxFunc(ctx, func(tx ill_db.IllRepo) error {
		r.IllRepo = tx
		return fn(r)
	})
}

func (r rotaSupplierBarrier) GetLocatedSuppliersByIllTransactionForUpdate(ctx common.ExtendedContext, id string) ([]ill_db.LocatedSupplier, error) {
	if r.beforeCheck {
		if err := r.pause(ctx); err != nil {
			return nil, err
		}
	}
	rows, err := r.IllRepo.GetLocatedSuppliersByIllTransactionForUpdate(ctx, id)
	if err == nil && !r.beforeCheck {
		err = r.pause(ctx)
	}
	return rows, err
}

func TestManualRotaConcurrentSupplierUpdate(t *testing.T) {
	for _, operation := range []string{"move", "add"} {
		for _, first := range []string{"protocol", "edit"} {
			t.Run(operation+"/"+first, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				f := newRotaFixture(t, "new", "new")
				f.ctx = common.CreateExtCtxWithArgs(ctx, nil)
				editResume, protocolResume := make(chan struct{}), make(chan struct{})
				releaseEdit := sync.OnceFunc(func() { close(editResume) })
				releaseProtocol := sync.OnceFunc(func() { close(protocolResume) })
				defer releaseEdit()
				defer releaseProtocol()
				barrier := rotaSupplierBarrier{rotaClosureBarrier{IllRepo: illRepo, beforeCheck: first == "protocol", paused: make(chan int, 1), resume: editResume}}
				handler := rotaHandler(barrier, f.directory)
				f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { handler.ServeHTTP(w, r.WithContext(ctx)) })
				editDone := make(chan *httptest.ResponseRecorder, 1)
				go func() {
					if operation == "move" {
						editDone <- f.move(1, -1)
					} else {
						editDone <- f.add(f.newSymbol)
					}
				}()
				editPID := awaitRotaPID(t, ctx, barrier.paused)
				protocolPIDs := make(chan int, 1)
				protocolDone := make(chan error, 1)
				expected := f.suppliers[1]
				expected.SupplierStatus = ill_db.SupplierStateSelectedPg
				if operation == "add" {
					expected.SupplierStatus = ill_db.SupplierStateSkippedPg
				}
				expected.PrevStatus = pgtype.Text{String: "ExpectToSupply", Valid: true}
				expected.LastStatus = pgtype.Text{String: "Loaned", Valid: true}
				expected.LastAction = pgtype.Text{String: "Request", Valid: true}
				expected.SupplierRequestID = pgtype.Text{String: "protocol-request", Valid: true}
				go func() {
					protocolDone <- illRepo.WithTxFunc(f.ctx, func(repo ill_db.IllRepo) error {
						pid := int(repo.(*ill_db.PgIllRepo).Tx.Conn().PgConn().PID())
						if first == "edit" {
							protocolPIDs <- pid
						}
						supplier, err := repo.GetLocatedSupplierByIdForUpdate(f.ctx, expected.ID)
						if err != nil {
							return err
						}
						supplier.SupplierStatus = expected.SupplierStatus
						supplier.PrevStatus = expected.PrevStatus
						supplier.LastStatus = expected.LastStatus
						supplier.LastAction = expected.LastAction
						supplier.SupplierRequestID = expected.SupplierRequestID
						if _, err := repo.SaveLocatedSupplier(f.ctx, ill_db.SaveLocatedSupplierParams(supplier)); err != nil {
							return err
						}
						if first == "protocol" {
							protocolPIDs <- pid
							select {
							case <-protocolResume:
							case <-ctx.Done():
								return ctx.Err()
							}
						}
						return nil
					})
				}()
				protocolPID := awaitRotaPID(t, ctx, protocolPIDs)
				if first == "protocol" {
					releaseEdit()
					requireRotaBlockedBy(t, ctx, editPID, protocolPID)
					releaseProtocol()
				} else {
					requireRotaBlockedBy(t, ctx, protocolPID, editPID)
					releaseEdit()
				}
				select {
				case err := <-protocolDone:
					require.NoError(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				select {
				case response := <-editDone:
					status := http.StatusOK
					if operation == "add" {
						status = http.StatusCreated
					} else if first == "protocol" {
						status = http.StatusConflict
					}
					require.Equal(t, status, response.Code, response.Body.String())
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if first == "edit" {
					if operation == "move" {
						expected.Ordinal = 0
					} else {
						expected.Ordinal = 3
					}
				}
				actual, err := illRepo.GetLocatedSupplierByIllTransactionAndSymbol(f.ctx, f.id, expected.SupplierSymbol)
				require.NoError(t, err)
				require.Equal(t, expected, actual)
			})
		}
	}
}
