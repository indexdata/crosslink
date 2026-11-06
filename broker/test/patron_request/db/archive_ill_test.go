package db

import (
	"encoding/json"
	"errors"
	"fmt"
	schedapi "github.com/indexdata/crosslink/broker/scheduler/api"
	sched_db "github.com/indexdata/crosslink/broker/scheduler/db"
	schedoapi "github.com/indexdata/crosslink/broker/scheduler/oapi"
	"github.com/indexdata/crosslink/broker/tenant"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	schedservice "github.com/indexdata/crosslink/broker/scheduler/service"
	"github.com/stretchr/testify/require"
)

func transactionArchiveFixture(t *testing.T) (borrowingArchiveFixture, *ill_db.PgIllRepo, ill_db.IllTransactionArchiveCandidate) {
	t.Helper()
	f := createBorrowingArchiveFixture(t)
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	_, err := pool.Exec(appCtx, `DELETE FROM patron_request WHERE id=$1`, f.pr)
	require.NoError(t, err)
	repo := ill_db.CreateIllRepo(pool).(*ill_db.PgIllRepo)
	return f, repo, transactionCandidate(t, repo, f.transaction)
}
func transactionCandidate(t *testing.T, repo *ill_db.PgIllRepo, id string) ill_db.IllTransactionArchiveCandidate {
	t.Helper()
	query, err := ill_db.ParseIllTransactionsCql(fmt.Sprintf(`id == "%s"`, id), 2)
	require.NoError(t, err)
	rows, err := repo.ListIllTransactionArchiveCandidates(appCtx, 100, "", query)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0]
}
func TestArchiveIllTransactionBundleAndLendingPreservation(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		t.Run(fmt.Sprintf("anonymous=%v", anonymous), func(t *testing.T) {
			f, repo, candidate := transactionArchiveFixture(t)
			stored, err := repo.ArchiveIllTransaction(appCtx, candidate, f.owner, time.Now(), anonymous)
			require.NoError(t, err)
			require.True(t, stored)
			var raw []byte
			var owner, method string
			var version int
			require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT bundle,owner,schema_version,COALESCE(anonymization_method,'') FROM archived_resource WHERE resource_type='ill-transaction' AND resource_id=$1`, f.transaction).Scan(&raw, &owner, &version, &method))
			require.Equal(t, f.owner, owner)
			require.Equal(t, 1, version)
			var bundle map[string]any
			require.NoError(t, json.Unmarshal(raw, &bundle))
			require.Len(t, bundle, 3)
			require.Len(t, bundle["illTransactionEvents"], 1)
			require.Len(t, bundle["locatedSuppliers"], 1)
			require.NotContains(t, string(raw), f.lendingEvent)
			if anonymous {
				require.NotContains(t, string(raw), "Jane")
				require.NotContains(t, string(raw), "987654321")
				require.NotContains(t, string(raw), f.transaction)
				require.Equal(t, "operational-allowlist-v1", method)
			} else {
				require.Contains(t, string(raw), "Jane supplier note")
				require.Contains(t, string(raw), f.transactionEvent)
			}
			var count int
			for _, table := range []string{"ill_transaction", "located_supplier", "event"} {
				field := "ill_transaction_id"
				if table == "ill_transaction" {
					field = "id"
				}
				require.NoError(t, repo.Pool.QueryRow(appCtx, "SELECT count(*) FROM "+table+" WHERE "+field+"=$1", f.transaction).Scan(&count))
				require.Zero(t, count)
			}
			require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT count(*) FROM patron_request WHERE id=$1`, f.lending).Scan(&count))
			require.Equal(t, 1, count)
			var transactionID string
			require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT ill_transaction_id FROM event WHERE id=$1`, f.lendingEvent).Scan(&transactionID))
			require.Equal(t, events.DEFAULT_ILL_TRANSACTION_ID, transactionID)
			stored, err = repo.ArchiveIllTransaction(appCtx, candidate, f.owner, time.Now(), anonymous)
			require.NoError(t, err)
			require.False(t, stored)
		})
	}
}
func TestArchiveIllTransactionEligibility(t *testing.T) {
	for _, condition := range []string{"borrowing", "owner", "age", "changed", "lending active", "transaction task", "lending task", "consumer work", "availability", "locked event"} {
		t.Run(condition, func(t *testing.T) {
			f, repo, candidate := transactionArchiveFixture(t)
			owner, cutoff := f.owner, time.Now()
			switch condition {
			case "borrowing":
				_, err := repo.Pool.Exec(appCtx, `INSERT INTO patron_request (id,state,side,requester_symbol,requester_req_id) SELECT $1,'CLOSED','borrowing',requester_symbol,requester_request_id FROM ill_transaction WHERE id=$2`, f.pr, f.transaction)
				require.NoError(t, err)
			case "owner":
				owner = "ISIL:WRONG"
			case "age":
				cutoff = f.updatedAt.Add(-time.Second)
			case "changed":
				_, err := repo.Pool.Exec(appCtx, `UPDATE ill_transaction SET last_supplier_status='LoanCompleted' WHERE id=$1`, f.transaction)
				require.NoError(t, err)
			case "lending active":
				_, err := repo.Pool.Exec(appCtx, `UPDATE patron_request SET terminal_state=false WHERE id=$1`, f.lending)
				require.NoError(t, err)
			case "transaction task", "lending task":
				id := f.transactionEvent
				if condition == "lending task" {
					id = f.lendingEvent
				}
				_, err := repo.Pool.Exec(appCtx, `UPDATE event SET event_status='PROCESSING' WHERE id=$1`, id)
				require.NoError(t, err)
			case "consumer work":
				_, err := repo.Pool.Exec(appCtx, `INSERT INTO event_consumer_work(event_id,signal,claimed) VALUES ($1,'TASK_COMPLETE',true)`, f.transactionEvent)
				require.NoError(t, err)
			case "availability":
				_, err := repo.Pool.Exec(appCtx, `INSERT INTO availability_attempt (ill_transaction_id,token,located_supplier_id) SELECT $1::varchar,'pending',id FROM located_supplier WHERE ill_transaction_id=$1 LIMIT 1`, f.transaction)
				require.NoError(t, err)
			case "locked event":
				tx, err := repo.Pool.Begin(appCtx)
				require.NoError(t, err)
				defer func() { _ = tx.Rollback(appCtx) }()
				_, err = tx.Exec(appCtx, `SELECT 1 FROM event WHERE id=$1 FOR UPDATE`, f.transactionEvent)
				require.NoError(t, err)
			}
			stored, err := repo.ArchiveIllTransaction(appCtx, candidate, owner, cutoff, false)
			require.NoError(t, err)
			require.False(t, stored)
			var count int
			require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT count(*) FROM archived_resource WHERE resource_id=$1`, f.transaction).Scan(&count))
			require.Zero(t, count)
		})
	}
}
func TestArchiveIllTransactionConcurrentAndRollback(t *testing.T) {
	f, repo, candidate := transactionArchiveFixture(t)
	// Force a deletion failure after inserting the archive and detaching events.
	_, err := repo.Pool.Exec(appCtx, `CREATE FUNCTION test_ill_archive_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'forced failure'; END; $$;
    CREATE TRIGGER test_ill_archive_failure BEFORE DELETE ON ill_transaction FOR EACH ROW EXECUTE FUNCTION test_ill_archive_failure();`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = repo.Pool.Exec(appCtx, `DROP TRIGGER IF EXISTS test_ill_archive_failure ON ill_transaction; DROP FUNCTION IF EXISTS test_ill_archive_failure();`)
	})
	_, err = repo.ArchiveIllTransaction(appCtx, candidate, f.owner, time.Now(), false)
	require.Error(t, err)
	var count int
	var transactionID string
	require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT count(*) FROM archived_resource WHERE resource_id=$1`, f.transaction).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT ill_transaction_id FROM event WHERE id=$1`, f.lendingEvent).Scan(&transactionID))
	require.Equal(t, f.transaction, transactionID)
	_, err = repo.Pool.Exec(appCtx, `DROP TRIGGER test_ill_archive_failure ON ill_transaction`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			stored, err := repo.ArchiveIllTransaction(appCtx, candidate, f.owner, time.Now(), false)
			results <- stored
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	successes := 0
	for result := range results {
		if result {
			successes++
		}
	}
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, successes)
}
func TestArchiveIllTransactionConcurrentBorrowingInsert(t *testing.T) {
	f, repo, candidate := transactionArchiveFixture(t)
	tx, err := repo.Pool.Begin(appCtx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(appCtx) }()
	// The trigger retains a parent lock until the new borrowing PR commits.
	_, err = tx.Exec(appCtx, `INSERT INTO patron_request (id,state,side,requester_symbol,requester_req_id) SELECT $1,'NEW','borrowing',requester_symbol,requester_request_id FROM ill_transaction WHERE id=$2`, f.pr, f.transaction)
	require.NoError(t, err)
	stored, err := repo.ArchiveIllTransaction(appCtx, candidate, f.owner, time.Now(), false)
	require.NoError(t, err)
	require.False(t, stored)
	require.NoError(t, tx.Commit(appCtx))
	stored, err = repo.ArchiveIllTransaction(appCtx, candidate, f.owner, time.Now(), false)
	require.NoError(t, err)
	require.False(t, stored)
}
func TestArchiveIllTransactionBatchOwnerScope(t *testing.T) {
	f, repo, _ := transactionArchiveFixture(t)
	_, err := repo.Pool.Exec(appCtx, `UPDATE ill_transaction SET last_supplier_status='LoanCompleted' WHERE id=$1`, f.transaction)
	require.NoError(t, err)
	svc := schedservice.NewBatchActionService(nil, nil, nil, nil).WithIllTransactionArchiveRepo(repo)
	event := events.Event{EventData: events.EventData{CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{ActionName: "archive-ill-transactions", Owner: uuid.NewString(), Selector: "last_supplier_status==LoanCompleted"}}, CustomData: map[string]any{"interval": "24h"}}}
	status, result := svc.ArchiveIllTransactions(appCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 0 ILL")
	event.EventData.BatchActionData.Owner = f.owner
	status, result = svc.ArchiveIllTransactions(appCtx, event)
	require.Equal(t, events.EventStatusSuccess, status)
	require.Contains(t, result.Note, "archived 1 ILL")
}

func TestArchiveIllTransactionManualRun(t *testing.T) {
	f, repo, _ := transactionArchiveFixture(t)
	_, err := repo.Pool.Exec(appCtx, `UPDATE ill_transaction SET last_supplier_status='LoanCompleted' WHERE id=$1`, f.transaction)
	require.NoError(t, err)
	schedRepo := sched_db.CreateSchedRepo(repo.Pool)
	id := uuid.NewString()
	task, err := schedRepo.SaveScheduledTask(appCtx, sched_db.SaveScheduledTaskParams{
		ID: id, EventName: events.EventNameInvokeBatchAction, Owner: f.owner, Title: pgtype.Text{String: "Manual transaction archive", Valid: true},
		Schedule: "FREQ=DAILY", Status: sched_db.ScheduledTaskStatusStopped,
		CreatedAt:  pgtype.Timestamptz{Time: time.Now(), Valid: true},
		ActionData: events.EventData{CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{ActionName: "archive-ill-transactions", Owner: f.owner, Selector: "last_supplier_status==LoanCompleted", TaskId: id}}, CustomData: map[string]any{"interval": "24h"}},
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = repo.Pool.Exec(appCtx, `DELETE FROM event WHERE event_data->'batchActionData'->>'taskId'=$1`, id)
		_, _ = repo.Pool.Exec(appCtx, `DELETE FROM scheduled_task WHERE id=$1`, id)
	})
	h := schedapi.NewSchedulerApiHandler(10, schedRepo, eventRepo, tenant.NewResolver().WithIllRepo(repo), nil)
	rr := httptest.NewRecorder()
	h.PostBatchActionsIdRun(rr, httptest.NewRequest(http.MethodPost, "/batch_actions/"+id+"/run", nil), id, schedoapi.PostBatchActionsIdRunParams{})
	require.Equal(t, http.StatusAccepted, rr.Code, rr.Body.String())
	require.Eventually(t, func() bool {
		list, err := eventRepo.GetBatchActionEvents(appCtx, id)
		return err == nil && len(list) == 1 && list[0].EventStatus == events.EventStatusSuccess
	}, 5*time.Second, 10*time.Millisecond)
	saved, err := schedRepo.GetScheduledTaskById(appCtx, id, nil)
	require.NoError(t, err)
	require.Equal(t, task.Status, saved.Status)
	require.Equal(t, task.Schedule, saved.Schedule)
	require.Equal(t, task.RunAt, saved.RunAt)
	var count int
	require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT count(*) FROM archived_resource WHERE resource_type='ill-transaction' AND resource_id=$1`, f.transaction).Scan(&count))
	require.Equal(t, 1, count)
}

func TestManualBatchRunQueueRollback(t *testing.T) {
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	repo := sched_db.CreateSchedRepo(pool)
	id := uuid.NewString()
	task := sched_db.ScheduledTask{ActionData: events.EventData{CommonEventData: events.CommonEventData{BatchActionData: &events.BatchActionData{TaskId: id, ActionName: "archive-ill-transactions"}}}}
	rollback := errors.New("force queue rollback")
	err := repo.WithTxFunc(appCtx, func(txRepo sched_db.SchedRepo) error {
		if err := txRepo.QueueBatchActionRun(appCtx, task); err != nil {
			return err
		}
		active, err := txRepo.HasActiveBatchActionEvents(appCtx, id)
		if err != nil {
			return err
		}
		require.True(t, active)
		var protected int
		err = txRepo.(*sched_db.PgSchedRepo).GetConnOrTx().QueryRow(appCtx, `SELECT count(*) FROM event_consumer_work w JOIN event e ON e.id=w.event_id WHERE e.event_data->'batchActionData'->>'taskId'=$1`, id).Scan(&protected)
		if err != nil {
			return err
		}
		require.Equal(t, 1, protected)
		return rollback
	})
	require.ErrorIs(t, err, rollback)
	list, err := eventRepo.GetBatchActionEvents(appCtx, id)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestArchiveIllTransactionBlocksConcurrentRequestLink(t *testing.T) {
	f, repo, candidate := transactionArchiveFixture(t)
	archiveTx, err := repo.Pool.Begin(appCtx)
	require.NoError(t, err)
	defer func() { _ = archiveTx.Rollback(appCtx) }()
	var archived bool
	require.NoError(t, archiveTx.QueryRow(appCtx, `SELECT archive_ill_transaction($1,$2,$3,$4,false)`, candidate.ID, f.owner, candidate.Version, time.Now()).Scan(&archived))
	require.True(t, archived)
	inserter, err := repo.Pool.Acquire(appCtx)
	require.NoError(t, err)
	defer inserter.Release()
	pid := inserter.Conn().PgConn().PID()
	var requestID string
	// The transaction is still visible to other sessions until archive commit.
	require.NoError(t, inserter.QueryRow(appCtx, `SELECT requester_request_id FROM ill_transaction WHERE id=$1`, f.transaction).Scan(&requestID))
	result := make(chan error, 1)
	go func() {
		_, err := inserter.Exec(appCtx, `INSERT INTO patron_request (id,state,side,requester_symbol,requester_req_id) VALUES ($1,'NEW','borrowing',$2,$3)`, f.pr, f.owner, requestID)
		result <- err
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := repo.Pool.QueryRow(appCtx, `SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&blocked)
		return err == nil && blocked
	}, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, archiveTx.Commit(appCtx))
	select {
	case err := <-result:
		var pgerr *pgconn.PgError
		require.ErrorAs(t, err, &pgerr)
		require.Equal(t, "40001", pgerr.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("request insertion did not finish after archive commit")
	}
}

func TestArchiveIllTransactionMigrationRoundTrip(t *testing.T) {
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	marker := uuid.NewString()
	_, err := pool.Exec(appCtx, `INSERT INTO archived_ill_transactions (ill_transaction,events,located_suppliers) VALUES (jsonb_build_object('id',$1::text),'[]','[]')`, marker)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(appCtx, `DELETE FROM archived_ill_transactions WHERE ill_transaction->>'id'=$1`, marker)
	})
	down, err := os.ReadFile("../../../migrations/076_archive_ill_transactions.down.sql")
	require.NoError(t, err)
	up, err := os.ReadFile("../../../migrations/076_archive_ill_transactions.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(appCtx, string(down))
	require.NoError(t, err)
	var legacy bool
	require.NoError(t, pool.QueryRow(appCtx, `SELECT to_regprocedure('archive_ill_transaction_by_date_and_status(timestamptz,text[])') IS NOT NULL`).Scan(&legacy))
	require.True(t, legacy)
	_, err = pool.Exec(appCtx, string(up))
	require.NoError(t, err)
	require.NoError(t, pool.QueryRow(appCtx, `SELECT to_regprocedure('archive_ill_transaction_by_date_and_status(timestamptz,text[])') IS NOT NULL`).Scan(&legacy))
	require.False(t, legacy)
	var count int
	require.NoError(t, pool.QueryRow(appCtx, `SELECT count(*) FROM archived_ill_transactions WHERE ill_transaction->>'id'=$1`, marker).Scan(&count))
	require.Equal(t, 1, count)
}
