package db

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	schedservice "github.com/indexdata/crosslink/broker/scheduler/service"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type borrowingArchiveFixture struct {
	pr, lending, transaction, borrowerEvent, transactionEvent, lendingEvent string
	owner                                                                   string
	updatedAt                                                               time.Time
}

func createBorrowingArchiveFixture(t *testing.T) borrowingArchiveFixture {
	t.Helper()
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	ctx := context.Background()
	f := borrowingArchiveFixture{
		pr: uuid.NewString(), lending: uuid.NewString(), transaction: uuid.NewString(),
		borrowerEvent: uuid.NewString(), transactionEvent: uuid.NewString(), lendingEvent: uuid.NewString(),
		owner: "ISIL:ARCHIVE-" + uuid.NewString(),
	}
	peer := uuid.NewString()
	reqID := uuid.NewString()
	old := time.Now().UTC().Add(-48 * time.Hour).Truncate(time.Microsecond)
	_, err := pool.Exec(ctx, `INSERT INTO peer (id,name,refresh_policy,url,vendor,broker_mode)
		VALUES ($1,'Archive supplier','never','https://example.org','mock','opaque')`, peer)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO patron_request
		(id,state,side,requester_symbol,requester_req_id,updated_at,internal_note,ill_request,terminal_state)
		VALUES ($1,'CLOSED','borrowing',$2,$3,$4,'Jane private note',
		'{"serviceInfo":{"serviceType":"Loan"},"patronInfo":{"patronId":987654321,"surname":"Jane"}}'::jsonb,true)`, f.pr, f.owner, reqID, old)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO patron_request
		(id,state,side,requester_symbol,supplier_symbol,requester_req_id,updated_at)
		VALUES ($1,'CLOSED','lending',$2,$3,$4,$5)`, f.lending, f.owner, "ISIL:OTHER", reqID, old)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO ill_transaction
		(id,timestamp,requester_symbol,requester_request_id,ill_transaction_data)
		VALUES ($1,$2,$3,$4,'{"serviceInfo":{"serviceType":"Loan"},"patronInfo":{"patronId":987654321,"surname":"Jane"}}'::jsonb)`, f.transaction, old, f.owner, reqID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO located_supplier
		(id,ill_transaction_id,supplier_id,supplier_symbol,note)
		VALUES ($1,$2,$3,'ISIL:OTHER','Jane supplier note')`, uuid.NewString(), f.transaction, peer)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO item (id,pr_id,barcode,created_at)
		VALUES ($1,$2,'Jane barcode',$3)`, uuid.NewString(), f.pr, old)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO notification
		(id,pr_id,from_symbol,to_symbol,note,created_at,cost,currency)
		VALUES ($1,$2,$3,'ISIL:OTHER','Jane notification',$4,12.50,'EUR')`, uuid.NewString(), f.pr, f.owner, old)
	require.NoError(t, err)
	for _, e := range []struct{ id, transaction, pr string }{
		{f.borrowerEvent, events.DEFAULT_ILL_TRANSACTION_ID, f.pr},
		{f.transactionEvent, f.transaction, events.DEFAULT_PATRON_REQUEST_ID},
		{f.lendingEvent, f.transaction, f.lending},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO event
			(id,timestamp,ill_transaction_id,patron_request_id,event_type,event_name,event_status,last_signal,event_data,result_data)
			VALUES ($1,$2,$3,$4,'TASK','invoke-action','SUCCESS','TASK_COMPLETE',
			'{"action":"close","note":"Jane message","user":"Jane","incomingMessage":{"patronInfo":{"patronId":987654321}},"outgoingMessage":{"note":"Jane"},"customData":{"patronId":987654321,"Jane@example.org":true}}'::jsonb,
			'{"actionResult":{"outcome":"success","toState":"CLOSED","childActionError":"Jane"},"customData":{"patronId":987654321,"Jane@example.org":true},"eventError":{"Message":"Jane"}}'::jsonb)`,
			e.id, old, e.transaction, e.pr)
		require.NoError(t, err)
	}
	f.updatedAt = old
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM archived_resource WHERE owner=$1`, f.owner)
		_, _ = pool.Exec(ctx, `DELETE FROM patron_request WHERE requester_symbol=$1`, f.owner)
		_, _ = pool.Exec(ctx, `DELETE FROM ill_transaction WHERE id=$1`, f.transaction)
		_, _ = pool.Exec(ctx, `DELETE FROM peer WHERE id=$1`, peer)
	})
	return f
}

func archiveFixtureRequest(f borrowingArchiveFixture) pr_db.PatronRequest {
	return pr_db.PatronRequest{ID: f.pr, State: "CLOSED", UpdatedAt: pgtype.Timestamp{Time: f.updatedAt, Valid: true}}
}

func TestArchiveBorrowingRequestPreservesLendingSide(t *testing.T) {
	f := createBorrowingArchiveFixture(t)
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	for _, tc := range []struct {
		owner  string
		cutoff time.Time
	}{
		{"ISIL:ANOTHER", time.Now()},
		{f.owner, f.updatedAt.Add(-time.Second)},
	} {
		stored, selectionErr := prRepo.(*pr_db.PgPrRepo).ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), tc.owner, tc.cutoff, false)
		require.NoError(t, selectionErr)
		require.False(t, stored)
	}
	archived, err := prRepo.(*pr_db.PgPrRepo).ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
	require.NoError(t, err)
	require.True(t, archived)
	var bundle []byte
	var version int
	var owner string
	err = pool.QueryRow(context.Background(), `SELECT bundle,schema_version,owner FROM archived_resource
		WHERE resource_type='borrowing-patron-request' AND resource_id=$1`, f.pr).Scan(&bundle, &version, &owner)
	require.NoError(t, err)
	require.Equal(t, 1, version)
	require.Equal(t, f.owner, owner)
	var data map[string]any
	require.NoError(t, json.Unmarshal(bundle, &data))
	for _, field := range []string{"patronRequestEvents", "illTransactionEvents", "locatedSuppliers", "items", "notifications"} {
		require.Len(t, data[field], 1, field)
	}
	require.NotNil(t, data["patronRequest"])
	require.NotNil(t, data["illTransaction"])
	require.Contains(t, string(bundle), "Jane private note")
	require.Contains(t, string(bundle), "987654321")
	require.NotContains(t, string(bundle), f.lending)
	require.NotContains(t, string(bundle), f.lendingEvent)
	var remaining int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM patron_request WHERE id IN ($1,$2)`, f.pr, f.lending).Scan(&remaining)
	require.NoError(t, err)
	require.Equal(t, 1, remaining)
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ill_transaction WHERE id=$1`, f.transaction).Scan(&remaining)
	require.NoError(t, err)
	require.Zero(t, remaining)
	var lendingTransaction string
	err = pool.QueryRow(context.Background(), `SELECT ill_transaction_id FROM event WHERE id=$1`, f.lendingEvent).Scan(&lendingTransaction)
	require.NoError(t, err)
	require.Equal(t, events.DEFAULT_ILL_TRANSACTION_ID, lendingTransaction)
}

func TestArchiveBorrowingRequestAnonymizesAndRollsBack(t *testing.T) {
	f := createBorrowingArchiveFixture(t)
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	_, err := pool.Exec(context.Background(), `INSERT INTO archived_resource
		(owner,resource_type,resource_id,schema_version,anonymized,bundle)
		VALUES ($1,'borrowing-patron-request',$2,1,false,'{}'::jsonb)`, f.owner, f.pr)
	require.NoError(t, err)
	_, err = prRepo.(*pr_db.PgPrRepo).ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), true)
	require.Error(t, err)
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM patron_request WHERE id=$1`, f.pr).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ill_transaction WHERE id=$1`, f.transaction).Scan(&count))
	require.Equal(t, 1, count)
	_, err = pool.Exec(context.Background(), `DELETE FROM archived_resource WHERE resource_type='borrowing-patron-request' AND resource_id=$1`, f.pr)
	require.NoError(t, err)
	archived, err := prRepo.(*pr_db.PgPrRepo).ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), true)
	require.NoError(t, err)
	require.True(t, archived)
	var bundle []byte
	var anonymized bool
	var method string
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT bundle,anonymized,anonymization_method FROM archived_resource WHERE resource_type='borrowing-patron-request' AND resource_id=$1`, f.pr).Scan(&bundle, &anonymized, &method))
	require.True(t, anonymized)
	require.Equal(t, "operational-allowlist-v1", method)
	require.NotContains(t, string(bundle), "Jane")
	require.NotContains(t, string(bundle), "987654321")
	var data map[string]any
	require.NoError(t, json.Unmarshal(bundle, &data))
	pr := data["patronRequest"].(map[string]any)
	require.Equal(t, "CLOSED", pr["state"])
	require.Equal(t, "borrowing", pr["side"])
	require.NotEmpty(t, pr["created_at"])
	require.NotEmpty(t, pr["updated_at"])
	require.NotContains(t, pr, "internal_note")
	require.NotContains(t, pr, "search")
	require.NotContains(t, pr, "items")
	request := pr["ill_request"].(map[string]any)
	require.NotContains(t, request, "patronInfo")
	require.Equal(t, "Loan", request["serviceInfo"].(map[string]any)["serviceType"])
	for _, field := range []string{"patronRequestEvents", "illTransactionEvents"} {
		event := data[field].([]any)[0].(map[string]any)
		require.Equal(t, "SUCCESS", event["event_status"])
		payload := event["event_data"].(map[string]any)
		require.Equal(t, "close", payload["action"])
		require.NotContains(t, payload, "customData")
		require.NotContains(t, payload, "incomingMessage")
		require.NotContains(t, payload, "outgoingMessage")
		require.NotContains(t, payload, "note")
		require.NotContains(t, payload, "user")
		result := event["result_data"].(map[string]any)
		require.Equal(t, "success", result["actionResult"].(map[string]any)["outcome"])
		require.NotContains(t, result, "eventError")
	}
	notification := data["notifications"].([]any)[0].(map[string]any)
	require.Equal(t, 12.5, notification["cost"])
	require.Equal(t, "EUR", notification["currency"])
	require.NotContains(t, notification, "note")
	item := data["items"].([]any)[0].(map[string]any)
	require.Equal(t, "UNKNOWN", item["lms_status"])
	require.NotContains(t, item, "barcode")
}

func TestArchiveBorrowingRequestSkipsActiveEvent(t *testing.T) {
	f := createBorrowingArchiveFixture(t)
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	_, err := pool.Exec(context.Background(), `UPDATE event SET event_status='PROCESSING' WHERE id=$1`, f.transactionEvent)
	require.NoError(t, err)
	stored, err := prRepo.(*pr_db.PgPrRepo).ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
	require.NoError(t, err)
	require.False(t, stored)
	var count int
	require.NoError(t, pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM patron_request WHERE id=$1`, f.pr).Scan(&count))
	require.Equal(t, 1, count)
	_, err = pool.Exec(context.Background(), `UPDATE event SET event_status='SUCCESS' WHERE id=$1`, f.transactionEvent)
	require.NoError(t, err)
}

func TestArchiveBorrowingRequestConcurrentRetry(t *testing.T) {
	f := createBorrowingArchiveFixture(t)
	results := make(chan bool, 2)
	errors := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			stored, err := prRepo.(*pr_db.PgPrRepo).ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
			results <- stored
			errors <- err
		}()
	}
	group.Wait()
	close(results)
	close(errors)
	count := 0
	for err := range errors {
		require.NoError(t, err)
	}
	for stored := range results {
		if stored {
			count++
		}
	}
	require.Equal(t, 1, count)
}

func TestBorrowingArchiveBatchContinuesPastBlockedCandidates(t *testing.T) {
	for _, mode := range []string{"active", "failed", "mixed"} {
		t.Run(mode, func(t *testing.T) {
			f := createBorrowingArchiveFixture(t)
			pool := prRepo.(*pr_db.PgPrRepo).Pool
			ctx := context.Background()
			// The shared fixture is outside this test's selector. All page candidates
			// deliberately share a creation timestamp to exercise the ID tie breaker.
			_, err := pool.Exec(ctx, `UPDATE patron_request SET terminal_state=false WHERE id=$1`, f.pr)
			require.NoError(t, err)
			count := 101
			if mode == "mixed" {
				count = 201
			}
			_, err = pool.Exec(ctx, `INSERT INTO patron_request
				(id,state,side,requester_symbol,requester_req_id,created_at,updated_at,ill_request,terminal_state)
				SELECT $1||'-candidate-'||lpad(i::text,3,'0'),'COMPLETED','borrowing',$2,
				$1||'-candidate-'||lpad(i::text,3,'0'),$3::timestamp,$3::timestamp,
				'{"serviceInfo":{"serviceType":"Loan"}}'::jsonb,true
				FROM generate_series(1,$4::int) i`, f.pr, f.owner, f.updatedAt, count)
			require.NoError(t, err)
			if mode == "failed" {
				_, err = pool.Exec(ctx, `INSERT INTO archived_resource
					(owner,resource_type,resource_id,schema_version,anonymized,bundle)
					SELECT $1,'borrowing-patron-request',$2||'-candidate-'||lpad(i::text,3,'0'),1,false,'{}'::jsonb
					FROM generate_series(1,100) i`, f.owner, f.pr)
			} else {
				_, err = pool.Exec(ctx, `INSERT INTO event
					(id,timestamp,ill_transaction_id,patron_request_id,event_type,event_name,event_status,last_signal)
					SELECT $1||'-event-'||i,$2::timestamp,$3,
					$1||'-candidate-'||lpad(i::text,3,'0'),'TASK','invoke-action','NEW',''
					FROM generate_series(1,100) i WHERE $4::text='active' OR i%2=0`,
					f.pr, f.updatedAt, events.DEFAULT_ILL_TRANSACTION_ID, mode)
			}
			require.NoError(t, err)
			archiveRepo := prRepo.(*pr_db.PgPrRepo)
			svc := schedservice.NewBatchActionService(nil, prRepo, nil, nil, archiveRepo)
			event := events.Event{EventData: events.EventData{
				CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{
					Owner:    f.owner,
					Selector: fmt.Sprintf(`terminal_state = true and side = borrowing and requester_symbol_exact = "%s" sortBy created_at/sort.descending`, f.owner),
				}},
				CustomData: map[string]any{"interval": "24h"},
			}}
			status, result := svc.ArchiveBorrowingRequests(appCtx, event)
			if mode == "failed" {
				require.Equal(t, events.EventStatusError, status)
				require.Len(t, result.CustomData, 100)
			} else {
				require.Equal(t, events.EventStatusSuccess, status)
			}
			expectedArchived := 1
			lastArchived := "101"
			if mode == "mixed" {
				expectedArchived, lastArchived = 100, "150"
			}
			require.Contains(t, result.Note, fmt.Sprintf("archived %d borrowing requests", expectedArchived))
			var remaining int
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM patron_request
				WHERE requester_symbol=$1 AND side='borrowing' AND terminal_state=true`, f.owner).Scan(&remaining))
			require.Equal(t, count-expectedArchived, remaining)
			require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM patron_request WHERE id=$1`, f.pr+"-candidate-"+lastArchived).Scan(&remaining))
			require.Zero(t, remaining)
			if mode == "active" {
				_, result = svc.ArchiveBorrowingRequests(appCtx, event)
				require.Contains(t, result.Note, "archived 0 borrowing requests; skipped 100")
			}
		})
	}
}
