package db

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	schedservice "github.com/indexdata/crosslink/broker/scheduler/service"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

type lendingArchiveFixture struct {
	borrowingArchiveFixture
	supplierOwner, item, notification string
}

func createLendingArchiveFixture(t *testing.T) lendingArchiveFixture {
	t.Helper()
	f := lendingArchiveFixture{
		borrowingArchiveFixture: createBorrowingArchiveFixture(t),
		supplierOwner:           "ISIL:LENDING-" + uuid.NewString(),
		item:                    uuid.NewString(), notification: uuid.NewString(),
	}
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	ctx := context.Background()
	_, err := pool.Exec(ctx, `UPDATE patron_request SET supplier_symbol=$2,
		ill_request='{"serviceInfo":{"serviceType":"Loan"},"patronInfo":{"surname":"Jane","patronId":987654321}}'::jsonb,
		internal_note='Jane lending note',terminal_state=true,created_at=$3 WHERE id=$1`, f.lending, f.supplierOwner, f.updatedAt)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO item(id,pr_id,barcode,created_at)
		VALUES($1,$2,'Jane lending barcode',$3)`, f.item, f.lending, f.updatedAt)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO notification(id,pr_id,from_symbol,to_symbol,note,cost,currency,receipt,created_at)
		VALUES($1,$2,$3,$4,'Jane lending notification',5,'EUR','SENT',$5)`, f.notification, f.lending, f.supplierOwner, f.owner, f.updatedAt)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM archived_resource WHERE owner=$1`, f.supplierOwner)
	})
	return f
}

func lendingFixtureRequest(f lendingArchiveFixture) pr_db.PatronRequest {
	return pr_db.PatronRequest{ID: f.lending, State: "CLOSED", UpdatedAt: pgtype.Timestamp{Time: f.updatedAt, Valid: true}}
}

func TestArchiveLendingRequestOwnershipAndCompleteness(t *testing.T) {
	f := createLendingArchiveFixture(t)
	repo := prRepo.(*pr_db.PgPrRepo)
	for _, tc := range []struct {
		pr     pr_db.PatronRequest
		owner  string
		cutoff time.Time
	}{
		{lendingFixtureRequest(f), f.owner, time.Now()}, // requester ownership grants no lending access
		{lendingFixtureRequest(f), "ISIL:OTHER", time.Now()},
		{lendingFixtureRequest(f), f.supplierOwner, f.updatedAt.Add(-time.Second)},
		{archiveFixtureRequest(f.borrowingArchiveFixture), f.supplierOwner, time.Now()},
	} {
		stored, err := repo.ArchiveLendingRequest(appCtx, tc.pr, tc.owner, tc.cutoff, false)
		require.NoError(t, err)
		require.False(t, stored)
	}
	stale := lendingFixtureRequest(f)
	stale.State = "NEW"
	stored, err := repo.ArchiveLendingRequest(appCtx, stale, f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.False(t, stored)
	stored, err = repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.True(t, stored)
	var bundle []byte
	var owner string
	var version int
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT bundle,owner,schema_version FROM archived_resource
		WHERE resource_type='lending-patron-request' AND resource_id=$1`, f.lending).Scan(&bundle, &owner, &version))
	require.Equal(t, f.supplierOwner, owner)
	require.Equal(t, 1, version)
	var data map[string]any
	require.NoError(t, json.Unmarshal(bundle, &data))
	require.Len(t, data, 4)
	require.Equal(t, "lending", data["patronRequest"].(map[string]any)["side"])
	for _, field := range []string{"patronRequestEvents", "items", "notifications"} {
		require.Len(t, data[field], 1, field)
	}
	require.Contains(t, string(bundle), "Jane lending note")
	require.NotContains(t, string(bundle), f.pr)
	require.NotContains(t, string(bundle), f.borrowerEvent)
	require.NotContains(t, string(bundle), f.transactionEvent)
	_, err = prRepo.GetPatronRequestById(appCtx, f.lending)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = prRepo.GetItemById(appCtx, f.item)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = prRepo.GetNotificationById(appCtx, f.notification)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = eventRepo.GetEvent(appCtx, f.lendingEvent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	// This fixture deliberately uses a numeric patron ID in protocol JSON, so
	// inspect the preserved request through SQL rather than typed protocol decoding.
	var count int
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM patron_request WHERE id=$1`, f.pr).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ill_transaction WHERE id=$1`, f.transaction).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM located_supplier WHERE ill_transaction_id=$1`, f.transaction).Scan(&count))
	require.Equal(t, 1, count)
	for _, id := range []string{f.borrowerEvent, f.transactionEvent} {
		require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM event WHERE id=$1`, id).Scan(&count))
		require.Equal(t, 1, count)
	}
}

func TestArchiveLendingRequestAnonymization(t *testing.T) {
	f := createLendingArchiveFixture(t)
	repo := prRepo.(*pr_db.PgPrRepo)
	stored, err := repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), true)
	require.NoError(t, err)
	require.True(t, stored)
	var bundle []byte
	var method string
	var anonymized bool
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT bundle,anonymized,anonymization_method FROM archived_resource
		WHERE resource_type='lending-patron-request' AND resource_id=$1`, f.lending).Scan(&bundle, &anonymized, &method))
	require.True(t, anonymized)
	require.Equal(t, "operational-allowlist-v1", method)
	require.NotContains(t, string(bundle), "Jane")
	require.NotContains(t, string(bundle), "987654321")
	var data map[string]any
	require.NoError(t, json.Unmarshal(bundle, &data))
	require.Len(t, data, 4)
	require.NotContains(t, data, "illTransaction")
	require.NotContains(t, data, "illTransactionEvents")
	require.NotContains(t, data, "locatedSuppliers")
	request := data["patronRequest"].(map[string]any)
	require.Equal(t, "lending", request["side"])
	require.Equal(t, "CLOSED", request["state"])
	require.NotEmpty(t, request["updated_at"])
	entry := data["patronRequestEvents"].([]any)[0].(map[string]any)
	require.Equal(t, "SUCCESS", entry["event_status"])
	require.NotContains(t, entry["event_data"].(map[string]any), "customData")
}

func TestArchiveLendingRequestRollbackAndRetry(t *testing.T) {
	f := createLendingArchiveFixture(t)
	repo := prRepo.(*pr_db.PgPrRepo)
	t.Cleanup(func() {
		_, _ = repo.Pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS test_fail_lending_archive_delete ON patron_request;
			DROP FUNCTION IF EXISTS test_fail_lending_archive_delete();`)
	})
	_, err := repo.Pool.Exec(context.Background(), `CREATE FUNCTION test_fail_lending_archive_delete() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			IF OLD.side = 'lending' THEN RAISE EXCEPTION 'forced delete failure'; END IF;
			RETURN OLD;
		END;
		$$;
		CREATE TRIGGER test_fail_lending_archive_delete BEFORE DELETE ON patron_request
		FOR EACH ROW EXECUTE FUNCTION test_fail_lending_archive_delete();`)
	require.NoError(t, err)
	_, err = repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.Error(t, err)
	_, err = prRepo.GetItemById(appCtx, f.item)
	require.NoError(t, err)
	_, err = prRepo.GetNotificationById(appCtx, f.notification)
	require.NoError(t, err)
	var count int
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM archived_resource
		WHERE resource_type='lending-patron-request' AND resource_id=$1`, f.lending).Scan(&count))
	require.Zero(t, count, "archive insertion must roll back when live deletion fails")
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM patron_request WHERE id=$1`, f.lending).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, repo.Pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM event WHERE id=$1`, f.lendingEvent).Scan(&count))
	require.Equal(t, 1, count)
	_, err = repo.Pool.Exec(context.Background(), `DROP TRIGGER test_fail_lending_archive_delete ON patron_request`)
	require.NoError(t, err)
	stored, err := repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.True(t, stored)
	stored, err = repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.False(t, stored)
}

func TestArchiveLendingRequestOnlyChecksOwnedActiveEvents(t *testing.T) {
	f := createLendingArchiveFixture(t)
	repo := prRepo.(*pr_db.PgPrRepo)
	_, err := repo.Pool.Exec(context.Background(), `UPDATE event SET event_status='PROCESSING' WHERE id=$1`, f.lendingEvent)
	require.NoError(t, err)
	stored, err := repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.False(t, stored)
	_, err = repo.Pool.Exec(context.Background(), `UPDATE event SET event_status='SUCCESS' WHERE id=$1`, f.lendingEvent)
	require.NoError(t, err)
	_, err = repo.Pool.Exec(context.Background(), `UPDATE event SET event_status='PROCESSING' WHERE id IN ($1,$2)`, f.borrowerEvent, f.transactionEvent)
	require.NoError(t, err)
	stored, err = repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.True(t, stored)
}

func TestArchiveLendingRequestConcurrentExecution(t *testing.T) {
	for _, bothSides := range []bool{false, true} {
		t.Run(map[bool]string{false: "same-side", true: "both-sides"}[bothSides], func(t *testing.T) {
			f := createLendingArchiveFixture(t)
			repo := prRepo.(*pr_db.PgPrRepo)
			results := make(chan bool, 2)
			errs := make(chan error, 2)
			var group sync.WaitGroup
			for i := range 2 {
				group.Add(1)
				go func() {
					defer group.Done()
					var stored bool
					var err error
					if bothSides && i == 0 {
						stored, err = repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f.borrowingArchiveFixture), f.owner, time.Now(), false)
					} else {
						stored, err = repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
					}
					results <- stored
					errs <- err
				}()
			}
			group.Wait()
			close(results)
			close(errs)
			count := 0
			for err := range errs {
				require.NoError(t, err)
			}
			for stored := range results {
				if stored {
					count++
				}
			}
			expected := 1
			if bothSides {
				// Borrowing can skip while lending holds its request lock.
				// Retrying after lending completes must archive the borrower too.
				if count == 1 {
					stored, err := repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f.borrowingArchiveFixture), f.owner, time.Now(), false)
					require.NoError(t, err)
					require.True(t, stored)
					count++
				}
				expected = 2
			}
			require.Equal(t, expected, count)
		})
	}
}

func TestLendingArchiveBatchContinuesPastBlockedCandidates(t *testing.T) {
	f := createLendingArchiveFixture(t)
	repo := prRepo.(*pr_db.PgPrRepo)
	ctx := context.Background()
	_, err := repo.Pool.Exec(ctx, `UPDATE patron_request SET terminal_state=false WHERE id=$1`, f.lending)
	require.NoError(t, err)
	_, err = repo.Pool.Exec(ctx, `INSERT INTO patron_request
		(id,state,side,requester_symbol,supplier_symbol,created_at,updated_at,ill_request,terminal_state)
		SELECT $1||'-candidate-'||lpad(i::text,3,'0'),'COMPLETED','lending',$2,$3,
		$4::timestamp,$4::timestamp,'{"serviceInfo":{"serviceType":"Loan"}}'::jsonb,true
		FROM generate_series(1,101) i`, f.lending, f.owner, f.supplierOwner, f.updatedAt)
	require.NoError(t, err)
	_, err = repo.Pool.Exec(ctx, `INSERT INTO event
		(id,timestamp,ill_transaction_id,patron_request_id,event_type,event_name,event_status,last_signal)
		SELECT $1||'-event-'||i,$2::timestamp,$3,$1||'-candidate-'||lpad(i::text,3,'0'),
		'TASK','invoke-action','NEW','' FROM generate_series(1,100) i`, f.lending, f.updatedAt, events.DEFAULT_ILL_TRANSACTION_ID)
	require.NoError(t, err)
	event := events.Event{EventData: events.EventData{
		CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{
			Owner:    f.supplierOwner,
			Selector: `terminal_state=true and side=lending and supplier_symbol_exact="` + f.supplierOwner + `"`,
		}},
		CustomData: map[string]any{"interval": "720h"},
	}}
	// The default interval is longer than this fixture's age, so it selects none.
	svc := schedservice.NewBatchActionService(nil, prRepo, nil, nil, repo)
	status, result := svc.ArchiveLendingRequests(appCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 0 lending requests")
	event.EventData.CustomData["interval"] = "24h"
	status, result = svc.ArchiveLendingRequests(appCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 1 lending requests; skipped 100")
	var count int
	require.NoError(t, repo.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM patron_request WHERE id=$1`, f.lending+"-candidate-101").Scan(&count))
	require.Zero(t, count)
}
