package db

import (
	"context"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestArchiveRequestSkipsPendingSignal(t *testing.T) {
	for _, tc := range []struct{ side, scope string }{
		{"borrowing", "request"},
		{"borrowing", "transaction"},
		{"borrowing", "other-request"},
		{"borrowing", "related-request"},
		{"lending", "request"},
	} {
		side := tc.side
		for _, signal := range []events.Signal{events.SignalNoticeCreated, events.SignalTaskComplete} {
			t.Run(side+"/"+tc.scope+"/"+string(signal), func(t *testing.T) {
				f := createLendingArchiveFixture(t)
				repo := prRepo.(*pr_db.PgPrRepo)
				request, owner, eventID := archiveFixtureRequest(f.borrowingArchiveFixture), f.owner, f.transactionEvent
				archive := repo.ArchiveBorrowingRequest
				switch tc.scope {
				case "request":
					eventID = f.borrowerEvent
				case "other-request":
					eventID = f.lendingEvent
				case "related-request":
					eventID = f.lendingEvent
					_, err := repo.Pool.Exec(appCtx, `UPDATE event SET ill_transaction_id=$2,event_name=$3 WHERE id=$1`,
						eventID, events.DEFAULT_ILL_TRANSACTION_ID, events.EventNameSendNotification)
					require.NoError(t, err)
				}
				if side == "lending" {
					request, owner, eventID = lendingFixtureRequest(f), f.supplierOwner, f.lendingEvent
					archive = repo.ArchiveLendingRequest
				}
				_, err := repo.Pool.Exec(appCtx, `UPDATE event SET last_signal=$2 WHERE id=$1`, eventID, signal)
				require.NoError(t, err)
				stored, err := archive(appCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.False(t, stored, "SUCCESS with an undispatched signal is still active")

				claimed, err := eventRepo.ClaimEventForSignal(appCtx, eventID, signal)
				require.NoError(t, err)
				require.Empty(t, claimed.LastSignal)
				_, err = eventRepo.ClaimEventForSignal(appCtx, eventID, signal)
				require.ErrorIs(t, err, pgx.ErrNoRows, "duplicate dispatch must not claim twice")
				// Claiming clears the pending signal. Archival deliberately does not
				// track consumer handlers through completion.
				stored, err = archive(appCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.True(t, stored)
			})
		}
	}
}

func TestArchiveRequestRechecksPendingSignalAfterEventLock(t *testing.T) {
	for _, tc := range []struct{ side, scope string }{
		{"borrowing", "transaction"},
		{"borrowing", "related-request"},
		{"lending", "request"},
	} {
		side := tc.side
		t.Run(side+"/"+tc.scope, func(t *testing.T) {
			f := createLendingArchiveFixture(t)
			repo := prRepo.(*pr_db.PgPrRepo)
			request, owner, eventID := archiveFixtureRequest(f.borrowingArchiveFixture), f.owner, f.transactionEvent
			if tc.scope == "related-request" {
				eventID = f.lendingEvent
				_, err := repo.Pool.Exec(appCtx, `UPDATE event SET ill_transaction_id=$2,event_name=$3 WHERE id=$1`,
					eventID, events.DEFAULT_ILL_TRANSACTION_ID, events.EventNameSendNotification)
				require.NoError(t, err)
			}
			if side == "lending" {
				request, owner, eventID = lendingFixtureRequest(f), f.supplierOwner, f.lendingEvent
			}
			ctx, cancel := context.WithTimeout(appCtx, 5*time.Second)
			defer cancel()
			extCtx := common.CreateExtCtxWithArgs(ctx, nil)
			publisher, err := repo.Pool.Begin(extCtx)
			require.NoError(t, err)
			defer func() { _ = publisher.Rollback(appCtx) }()
			_, err = publisher.Exec(extCtx, `UPDATE event SET last_signal=$2 WHERE id=$1`, eventID, events.SignalNoticeCreated)
			require.NoError(t, err)
			pid := make(chan int, 1)
			result := make(chan error, 1)
			var stored bool
			go func() {
				result <- repo.WithTxFunc(extCtx, func(txRepo pr_db.PrRepo) error {
					tx := txRepo.(*pr_db.PgPrRepo)
					var backendPID int
					if err := tx.GetConnOrTx().QueryRow(extCtx, `SELECT pg_backend_pid()`).Scan(&backendPID); err != nil {
						return err
					}
					pid <- backendPID
					archive := tx.ArchiveBorrowingRequest
					if side == "lending" {
						archive = tx.ArchiveLendingRequest
					}
					var err error
					stored, err = archive(extCtx, request, owner, time.Now(), false)
					return err
				})
			}()
			var backendPID int
			select {
			case backendPID = <-pid:
			case <-ctx.Done():
				t.Fatal("archiver did not start")
			}
			require.Eventually(t, func() bool {
				var waiting bool
				err := repo.Pool.QueryRow(extCtx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, backendPID).Scan(&waiting)
				return err == nil && waiting
			}, 3*time.Second, 10*time.Millisecond, "archiver must reach the event lock after seeing no committed pending signal")
			require.NoError(t, publisher.Commit(extCtx))
			require.NoError(t, <-result)
			require.False(t, stored, "final check must observe the newly committed signal")
		})
	}
}

func TestLendingArchiveIgnoresTransactionPendingSignal(t *testing.T) {
	f := createLendingArchiveFixture(t)
	repo := prRepo.(*pr_db.PgPrRepo)
	_, err := repo.Pool.Exec(appCtx, `UPDATE event SET last_signal=$2 WHERE id=$1`, f.transactionEvent, events.SignalTaskComplete)
	require.NoError(t, err)
	stored, err := repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.True(t, stored)
	remaining, err := eventRepo.GetEvent(appCtx, f.transactionEvent)
	require.NoError(t, err)
	require.Equal(t, string(events.SignalTaskComplete), remaining.LastSignal, "lending archival must preserve transaction signals")
}
