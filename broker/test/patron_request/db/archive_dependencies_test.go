package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/stretchr/testify/require"
)

func createArchiveSuccessor(t *testing.T, f borrowingArchiveFixture, requestType string, terminal bool) string {
	t.Helper()
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	id := uuid.NewString()
	_, err := pool.Exec(appCtx, `INSERT INTO patron_request
		(id,state,side,requester_symbol,requester_req_id,prev_req_id,ill_request,terminal_state)
		VALUES ($1,'CUSTOM','borrowing',$2,$1,$3,
		jsonb_build_object('serviceInfo',jsonb_build_object('requestType',NULLIF($4::text,''))),$5)`,
		id, f.owner, f.pr, requestType, terminal)
	require.NoError(t, err)
	_, err = pool.Exec(appCtx, `UPDATE patron_request SET next_req_id=$2 WHERE id=$1`, f.pr, id)
	require.NoError(t, err)
	return id
}

func requireUnarchivedTransaction(t *testing.T, f borrowingArchiveFixture) {
	t.Helper()
	pool := prRepo.(*pr_db.PgPrRepo).Pool
	var requests, transactions, suppliers, archives int
	require.NoError(t, pool.QueryRow(appCtx, `SELECT
		(SELECT COUNT(*) FROM patron_request WHERE id=$1),
		(SELECT COUNT(*) FROM ill_transaction WHERE id=$2),
		(SELECT COUNT(*) FROM located_supplier WHERE ill_transaction_id=$2),
		(SELECT COUNT(*) FROM archived_resource WHERE resource_id=$1)`, f.pr, f.transaction).
		Scan(&requests, &transactions, &suppliers, &archives))
	require.Equal(t, 1, requests)
	require.Equal(t, 1, transactions)
	require.Equal(t, 1, suppliers)
	require.Zero(t, archives)
}

func TestArchiveBorrowingRequestWaitsForRetrySuccessor(t *testing.T) {
	for _, tc := range []struct {
		name, requestType      string
		terminal, deferArchive bool
	}{
		{"live-retry", "Retry", false, true},
		{"terminal-retry", "Retry", true, false},
		{"live-rerequest", "New", false, false},
		{"default-rerequest", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := createBorrowingArchiveFixture(t)
			successor := createArchiveSuccessor(t, f, tc.requestType, tc.terminal)
			repo := prRepo.(*pr_db.PgPrRepo)
			stored, err := repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
			require.NoError(t, err)
			require.Equal(t, !tc.deferArchive, stored)
			if tc.deferArchive {
				requireUnarchivedTransaction(t, f)
				_, err = repo.Pool.Exec(appCtx, `UPDATE patron_request SET terminal_state=true WHERE id=$1`, successor)
				require.NoError(t, err)
				stored, err = repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
				require.NoError(t, err)
				require.True(t, stored, "a terminal successor no longer needs the transaction")
			}
		})
	}
}

func TestArchiveBorrowingRequestAfterRetryHandoffPreservesTransaction(t *testing.T) {
	f := createBorrowingArchiveFixture(t)
	successor := createArchiveSuccessor(t, f, "Retry", false)
	repo := prRepo.(*pr_db.PgPrRepo)
	stored, err := repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
	require.NoError(t, err)
	require.False(t, stored)
	requireUnarchivedTransaction(t, f)
	// handleRetryRequest advances the requester ID under the transaction lock.
	_, err = repo.Pool.Exec(appCtx, `UPDATE ill_transaction
		SET prev_requester_request_id=requester_request_id,requester_request_id=$2 WHERE id=$1`, f.transaction, successor)
	require.NoError(t, err)
	stored, err = repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
	require.NoError(t, err)
	require.True(t, stored)
	var requestID string
	require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT requester_request_id FROM ill_transaction WHERE id=$1`, f.transaction).Scan(&requestID))
	require.Equal(t, successor, requestID)
	var suppliers int
	require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT COUNT(*) FROM located_supplier WHERE ill_transaction_id=$1`, f.transaction).Scan(&suppliers))
	require.Equal(t, 1, suppliers)
}

func TestArchiveBorrowingRequestWaitsForRelatedLending(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		terminal, otherRequester, otherRequest, deferArchive bool
	}{
		{"live-related", false, false, false, true},
		{"terminal-related", true, false, false, false},
		{"other-requester", false, true, false, false},
		{"other-request", false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := createBorrowingArchiveFixture(t)
			repo := prRepo.(*pr_db.PgPrRepo)
			// Cleanup by ID as well because this case can change the requester.
			t.Cleanup(func() { _, _ = repo.Pool.Exec(appCtx, `DELETE FROM patron_request WHERE id=$1`, f.lending) })
			_, err := repo.Pool.Exec(appCtx, `UPDATE patron_request SET terminal_state=$2,
				requester_symbol=CASE WHEN $3 THEN 'ISIL:UNRELATED' ELSE requester_symbol END,
				requester_req_id=CASE WHEN $4 THEN $1 ELSE requester_req_id END WHERE id=$1`,
				f.lending, tc.terminal, tc.otherRequester, tc.otherRequest)
			require.NoError(t, err)
			stored, err := repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
			require.NoError(t, err)
			require.Equal(t, !tc.deferArchive, stored)
			if tc.deferArchive {
				requireUnarchivedTransaction(t, f)
				_, err = repo.Pool.Exec(appCtx, `UPDATE patron_request SET terminal_state=true WHERE id=$1`, f.lending)
				require.NoError(t, err)
				stored, err = repo.ArchiveBorrowingRequest(appCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
				require.NoError(t, err)
				require.True(t, stored, "lending completion allows transaction archival")
			}
		})
	}
}

func TestArchiveBorrowingRequestSkipsContendedDependencies(t *testing.T) {
	for _, dependency := range []string{"retry", "lending"} {
		t.Run(dependency, func(t *testing.T) {
			f := createBorrowingArchiveFixture(t)
			relatedID := f.lending
			if dependency == "retry" {
				relatedID = createArchiveSuccessor(t, f, "Retry", true)
			}
			repo := prRepo.(*pr_db.PgPrRepo)
			holder, err := repo.Pool.Begin(appCtx)
			require.NoError(t, err)
			defer func() { _ = holder.Rollback(appCtx) }()
			_, err = holder.Exec(appCtx, `SELECT 1 FROM patron_request WHERE id=$1 FOR UPDATE`, relatedID)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(appCtx, 5*time.Second)
			defer cancel()
			extCtx := common.CreateExtCtxWithArgs(ctx, nil)
			stored, err := repo.ArchiveBorrowingRequest(extCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
			require.NoError(t, err)
			require.False(t, stored)
			requireUnarchivedTransaction(t, f)
			require.NoError(t, holder.Rollback(extCtx))
			stored, err = repo.ArchiveBorrowingRequest(extCtx, archiveFixtureRequest(f), f.owner, time.Now(), false)
			require.NoError(t, err)
			require.True(t, stored)
		})
	}
}
