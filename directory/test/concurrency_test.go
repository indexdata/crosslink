package test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/app"
	importdb "github.com/indexdata/crosslink/directory/import/db"
	"github.com/indexdata/crosslink/directory/import/model"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

var consortiumPermissionHeaders = map[string]string{
	"X-Okapi-Tenant":      "ANINST",
	"X-Okapi-Permissions": `["directory.consortium.all"]`,
}

type lockUnavailableTracer struct {
	observed chan struct{}
}

type pauseEntryLockTracer struct {
	entryID  uuid.UUID
	locked   chan struct{}
	release  <-chan struct{}
	conflict chan<- string
	once     sync.Once
}

type pauseEntryLockContextKey struct{}

func (t *pauseEntryLockTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "-- name: EntryByIdForUpdate") &&
		!strings.Contains(data.SQL, "-- name: EntryByIdForImportUpdate") && len(data.Args) > 0 {
		if entryID, ok := data.Args[0].(uuid.UUID); ok && entryID == t.entryID {
			return context.WithValue(ctx, pauseEntryLockContextKey{}, true)
		}
	}
	return ctx
}

func (t *pauseEntryLockTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var pgErr *pgconn.PgError
	if errors.As(data.Err, &pgErr) {
		select {
		case t.conflict <- pgErr.Code:
		default:
		}
	}
	if data.Err != nil || ctx.Value(pauseEntryLockContextKey{}) != true {
		return
	}
	t.once.Do(func() {
		close(t.locked)
		select {
		case <-t.release:
		case <-ctx.Done():
		}
	})
}

func (t lockUnavailableTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return ctx
}

func (t lockUnavailableTracer) TraceQueryEnd(_ context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	var pgErr *pgconn.PgError
	if errors.As(data.Err, &pgErr) && pgErr.Code == "55P03" {
		select {
		case t.observed <- struct{}{}:
		default:
		}
	}
}

func TestConcurrency(t *testing.T) {
	t.Run("ConcurrentEntryPatch", func(t *testing.T) {
		resetDb()

		var wg sync.WaitGroup
		wg.Add(2)

		var res1, res2 *http.Response
		var data1, data2 string

		// PATCH 1: Update symbols
		go func() {
			defer wg.Done()
			res1, data1 = jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000002",
				`{"symbols":[{"authority":"TEST","symbol":"CONCURRENT1"}]}`, consortiumPermissionHeaders)
		}()

		// PATCH 2: Update description
		go func() {
			defer wg.Done()
			res2, data2 = jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000002",
				`{"description":"Updated concurrently"}`, consortiumPermissionHeaders)
		}()

		wg.Wait()

		// Both should succeed
		if res1.StatusCode != http.StatusNoContent {
			t.Errorf("PATCH 1 failed: %d %s", res1.StatusCode, data1)
		}
		if res2.StatusCode != http.StatusNoContent {
			t.Errorf("PATCH 2 failed: %d %s", res2.StatusCode, data2)
		}

		// Verify both updates were applied (without FOR UPDATE, one will be lost)
		_, data := jsonReq(t, http.MethodGet, "/entries/by-id/00000000-0000-0000-0000-000000000002", "", consortiumPermissionHeaders)
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(data), &entry); err != nil {
			t.Fatalf("Failed to unmarshal entry: %v", err)
		}

		if desc, _ := entry["description"].(string); desc != "Updated concurrently" {
			t.Errorf("Description lost in race: got %v", entry["description"])
		}
		if symbols, ok := entry["symbols"].([]interface{}); !ok || len(symbols) == 0 {
			t.Errorf("Symbols lost in race: got %v", entry["symbols"])
		}
	})

	t.Run("EntryPatchDeleteRace", func(t *testing.T) {
		resetDb()

		var wg sync.WaitGroup
		wg.Add(2)

		var patchRes *http.Response
		var deleteRes *http.Response

		// PATCH: Update entry
		go func() {
			defer wg.Done()
			patchRes, _ = jsonReq(t, http.MethodPatch, "/entries/by-id/00000000-0000-0000-0000-000000000001",
				`{"name":"Updated"}`, consortiumPermissionHeaders)
		}()

		// DELETE: Remove same entry
		go func() {
			defer wg.Done()
			deleteRes, _ = jsonReq(t, http.MethodDelete, "/entries/by-id/00000000-0000-0000-0000-000000000001", "", consortiumPermissionHeaders)
		}()

		wg.Wait()

		// One should succeed, but without locking, behavior is undefined
		// With FOR UPDATE, they will serialize properly
		if patchRes.StatusCode == http.StatusNoContent && deleteRes.StatusCode == http.StatusNoContent {
			// Both succeeded - verify final state is consistent
			var count int
			err := dbpool.QueryRow(context.Background(),
				"SELECT COUNT(*) FROM entries WHERE id = '00000000-0000-0000-0000-000000000001'").Scan(&count)
			if err != nil || count != 0 {
				t.Error("Entry should be deleted if DELETE succeeded")
			}
		}
	})

	t.Run("ConcurrentConsortiumPosts", func(t *testing.T) {
		resetDb()
		_, err := dbpool.Exec(context.Background(), "UPDATE entries SET parent = NULL, type = 'Institution'")
		if err != nil {
			t.Fatalf("failed to prepare entries: %v", err)
		}

		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for _, name := range []string{"Consortium One", "Consortium Two"} {
			name := name
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, _ := jsonReq(t, http.MethodPost, "/entries", `{"name":"`+name+`","type":"Consortium"}`, consortiumPermissionHeaders)
				statuses <- res.StatusCode
			}()
		}
		wg.Wait()
		close(statuses)

		assertOneConsortiumWrite(t, statuses, http.StatusCreated)
	})

	t.Run("ConcurrentConsortiumPromotions", func(t *testing.T) {
		resetDb()
		_, err := dbpool.Exec(context.Background(), "UPDATE entries SET parent = NULL, type = 'Institution'")
		if err != nil {
			t.Fatalf("failed to prepare entries: %v", err)
		}

		var wg sync.WaitGroup
		statuses := make(chan int, 2)
		for _, id := range []string{"00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"} {
			id := id
			wg.Add(1)
			go func() {
				defer wg.Done()
				res, _ := jsonReq(t, http.MethodPatch, "/entries/by-id/"+id, `{"type":"Consortium"}`, consortiumPermissionHeaders)
				statuses <- res.StatusCode
			}()
		}
		wg.Wait()
		close(statuses)

		assertOneConsortiumWrite(t, statuses, http.StatusNoContent)
	})
}

func TestConcurrentImportDoesNotDeadlockEntryPatch(t *testing.T) {
	resetDb()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	parentID := "00000000-0000-0000-0000-000000000001"
	childID := "00000000-0000-0000-0000-000000000002"
	_, err := dbpool.Exec(ctx, `UPDATE entries SET parent=NULL, type='Institution' WHERE id=$1`, parentID)
	require.NoError(t, err)
	_, err = dbpool.Exec(ctx, `UPDATE entries SET parent=$1, type='Branch' WHERE id=$2`, parentID, childID)
	require.NoError(t, err)
	_, err = dbpool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'TEST', 'PARENT')`, parentID)
	require.NoError(t, err)

	blocker, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocker.Rollback(ctx) }()
	_, err = blocker.Exec(ctx, `SELECT id FROM entries WHERE id=$1 FOR UPDATE`, parentID)
	require.NoError(t, err)
	lockUnavailable := make(chan struct{}, 1)
	importPoolConfig, err := pgxpool.ParseConfig(app.ConnectionString)
	require.NoError(t, err)
	importPoolConfig.ConnConfig.Tracer = lockUnavailableTracer{observed: lockUnavailable}
	importPool, err := pgxpool.NewWithConfig(ctx, importPoolConfig)
	require.NoError(t, err)
	t.Cleanup(importPool.Close)

	parent := model.SymbolRef{Authority: "TEST", Symbol: "PARENT"}
	key := model.SymbolRef{Authority: "TEST", Symbol: "ANINST"}
	aggregate := model.EntryAggregate{
		Key: key,
		Data: model.EntryData{
			Name:      "Imported child",
			Type:      "Branch",
			Parent:    &parent,
			Symbols:   []model.SymbolRef{key},
			Endpoints: []model.ServiceEndpoint{},
			Addresses: []model.Address{},
			Closures:  []model.Closure{},
		},
	}
	importDone := make(chan error, 1)
	go func() {
		_, importErr := importdb.New(importPool).ImportEntry(ctx, aggregate, model.ConflictPolicyUpdate)
		importDone <- importErr
	}()

	select {
	case <-lockUnavailable:
	case <-ctx.Done():
		require.FailNow(t, "import did not report a non-waiting row-lock conflict", ctx.Err())
	}

	patchDone := make(chan *http.Response, 1)
	go func() {
		response, _ := jsonReq(t, http.MethodPatch, "/entries/by-id/"+childID,
			`{"parent":"`+parentID+`"}`, consortiumPermissionHeaders)
		patchDone <- response
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := dbpool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE datname=current_database()
				AND pid <> pg_backend_pid()
				AND wait_event_type='Lock'
				AND query LIKE '%EntryByIdForUpdate%'
				AND query NOT LIKE '%EntryByIdForImportUpdate%'
		)`).Scan(&waiting)
		return queryErr == nil && waiting
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, blocker.Commit(ctx))

	require.Equal(t, http.StatusNoContent, (<-patchDone).StatusCode)
	require.NoError(t, <-importDone)
}

func TestConcurrentAssignmentImportsDoNotDeadlockEntryPatch(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		importRun func(context.Context, *importdb.PgImportRepo, model.SymbolRef, model.SymbolRef) error
	}{
		{
			name: "tier",
			importRun: func(ctx context.Context, repo *importdb.PgImportRepo, consortium, member model.SymbolRef) error {
				_, err := repo.ImportTier(ctx, model.TierAggregate{
					Key:  model.TierKey{Consortium: consortium, Name: "Concurrent lock"},
					Data: model.TierData{Level: "standard", Type: "loan", Entries: []model.SymbolRef{member}},
				}, model.ConflictPolicyUpdate)
				return err
			},
		},
		{
			name: "network",
			importRun: func(ctx context.Context, repo *importdb.PgImportRepo, consortium, member model.SymbolRef) error {
				_, err := repo.ImportNetwork(ctx, model.NetworkAggregate{
					Key: model.NetworkKey{Consortium: consortium, Name: "Concurrent lock"},
					Data: model.NetworkData{Entries: []model.NetworkAssignment{{
						SymbolRef: member,
						Priority:  1,
					}}},
				}, model.ConflictPolicyUpdate)
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			resetDb()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			consortiumID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
			memberID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
			_, err := dbpool.Exec(ctx, `UPDATE entries SET parent=NULL, type='Institution'`)
			require.NoError(t, err)
			_, err = dbpool.Exec(ctx, `UPDATE entries SET type='Consortium' WHERE id=$1`, consortiumID)
			require.NoError(t, err)
			_, err = dbpool.Exec(ctx, `UPDATE entries SET parent=$1, type='Institution' WHERE id=$2`, consortiumID, memberID)
			require.NoError(t, err)
			_, err = dbpool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'TEST', 'LOCK-CON')`, consortiumID)
			require.NoError(t, err)

			firstLocked := make(chan struct{})
			resumeImport := make(chan struct{})
			var resumeOnce sync.Once
			importConflict := make(chan string, 1)
			tracer := &pauseEntryLockTracer{
				entryID: consortiumID, locked: firstLocked, release: resumeImport, conflict: importConflict,
			}
			importPoolConfig, err := pgxpool.ParseConfig(app.ConnectionString)
			require.NoError(t, err)
			importPoolConfig.ConnConfig.Tracer = tracer
			importPool, err := pgxpool.NewWithConfig(ctx, importPoolConfig)
			require.NoError(t, err)
			t.Cleanup(importPool.Close)
			t.Cleanup(func() { resumeOnce.Do(func() { close(resumeImport) }) })

			consortium := model.SymbolRef{Authority: "TEST", Symbol: "LOCK-CON"}
			member := model.SymbolRef{Authority: "TEST", Symbol: "ANINST"}
			importDone := make(chan error, 1)
			go func() {
				importDone <- testCase.importRun(ctx, importdb.New(importPool), consortium, member)
			}()

			select {
			case <-firstLocked:
			case <-ctx.Done():
				require.FailNow(t, "import did not acquire its first entry lock", ctx.Err())
			}

			type patchResult struct {
				response *http.Response
				body     string
			}
			patchDone := make(chan patchResult, 1)
			go func() {
				response, body := jsonReq(t, http.MethodPatch, "/entries/by-id/"+memberID.String(),
					`{"parent":"`+consortiumID.String()+`"}`, consortiumPermissionHeaders)
				patchDone <- patchResult{response: response, body: body}
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				queryErr := dbpool.QueryRow(ctx, `SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity
					WHERE datname=current_database()
						AND pid <> pg_backend_pid()
						AND wait_event_type='Lock'
						AND query LIKE '%EntryByIdForUpdate%'
						AND query NOT LIKE '%EntryByIdForImportUpdate%'
				)`).Scan(&waiting)
				return queryErr == nil && waiting
			}, 2*time.Second, 10*time.Millisecond)
			resumeOnce.Do(func() { close(resumeImport) })
			select {
			case code := <-importConflict:
				require.Equal(t, "55P03", code, "import must avoid waiting on its second entry lock")
			case <-ctx.Done():
				require.FailNow(t, "import did not report a non-waiting second-lock conflict", ctx.Err())
			}

			var patch patchResult
			select {
			case patch = <-patchDone:
			case <-ctx.Done():
				require.FailNow(t, "entry patch did not finish", ctx.Err())
			}
			require.Equal(t, http.StatusNoContent, patch.response.StatusCode, patch.body)
			select {
			case importErr := <-importDone:
				require.NoError(t, importErr)
			case <-ctx.Done():
				require.FailNow(t, "assignment import did not finish", ctx.Err())
			}
		})
	}
}

func assertOneConsortiumWrite(t *testing.T, statuses <-chan int, successStatus int) {
	t.Helper()
	successes, rejections := 0, 0
	for status := range statuses {
		switch status {
		case successStatus:
			successes++
		case http.StatusBadRequest:
			rejections++
		default:
			t.Errorf("unexpected response status: %d", status)
		}
	}
	if successes != 1 || rejections != 1 {
		t.Errorf("expected one success and one rejection, got %d successes and %d rejections", successes, rejections)
	}

	var count int
	if err := dbpool.QueryRow(context.Background(), "SELECT COUNT(*) FROM entries WHERE type = 'Consortium'").Scan(&count); err != nil {
		t.Fatalf("failed to count consortium entries: %v", err)
	}
	if count != 1 {
		t.Errorf("expected exactly one consortium entry, got %d", count)
	}
}
