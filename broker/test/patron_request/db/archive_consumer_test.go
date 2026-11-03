package db

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/events"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestArchiveRequestProtectsConsumerHandoff(t *testing.T) {
	for _, tc := range []struct{ side, scope string }{
		{"borrowing", "request"},
		{"borrowing", "transaction"},
		{"borrowing", "other-request"},
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
				}
				if side == "lending" {
					request, owner, eventID = lendingFixtureRequest(f), f.supplierOwner, f.lendingEvent
					archive = repo.ArchiveLendingRequest
				}
				_, err := repo.Pool.Exec(appCtx, `UPDATE event SET last_signal=$2 WHERE id=$1`, eventID, signal)
				require.NoError(t, err)
				// Prepare without dispatching so the pending and claimed gaps can be
				// exercised deterministically, using the publisher's actual SQL.
				queries := events.Queries{}
				require.NoError(t, queries.PrepareConsumerSignal(appCtx, repo.Pool, events.PrepareConsumerSignalParams{
					EventID: eventID, Signal: string(signal),
				}))
				stored, err := archive(appCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.False(t, stored, "SUCCESS with an undispatched signal is still active")

				claimed, err := eventRepo.ClaimEventForSignal(appCtx, eventID, signal)
				require.NoError(t, err)
				require.Empty(t, claimed.LastSignal)
				_, err = eventRepo.ClaimEventForSignal(appCtx, eventID, signal)
				require.ErrorIs(t, err, pgx.ErrNoRows, "duplicate dispatch must not claim twice")
				stored, err = archive(appCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.False(t, stored, "clearing last_signal must not expose the handoff gap")

				// A fresh repository (as after a restart) must see the same protection.
				freshRepo := app.CreateEventRepo(repo.Pool)
				var workCount int
				require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT COUNT(*) FROM event_consumer_work WHERE event_id=$1 AND claimed`, eventID).Scan(&workCount))
				require.Equal(t, 1, workCount)
				followupID := uuid.NewString()
				_, err = freshRepo.SaveEvent(appCtx, events.SaveEventParams{
					ID: followupID, Timestamp: claimed.Timestamp,
					IllTransactionID: claimed.IllTransactionID, PatronRequestID: claimed.PatronRequestID,
					ParentID: pgtype.Text{String: eventID, Valid: true}, EventType: events.EventTypeTask,
					EventName: events.EventNameConfirmRequesterMsg, EventStatus: events.EventStatusNew,
				})
				require.NoError(t, err)
				require.NoError(t, freshRepo.CompleteConsumerSignal(appCtx, eventID, signal))
				stored, err = archive(appCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.False(t, stored, "follow-up confirmation must survive until processed")
				_, err = repo.Pool.Exec(appCtx, `UPDATE event SET event_status='SUCCESS' WHERE id=$1`, followupID)
				require.NoError(t, err)
				stored, err = archive(appCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.True(t, stored)
			})
		}
	}
}

func TestArchiveRequestRechecksConsumerWorkAfterEventLock(t *testing.T) {
	for _, side := range []string{"borrowing", "lending"} {
		t.Run(side, func(t *testing.T) {
			f := createLendingArchiveFixture(t)
			repo := prRepo.(*pr_db.PgPrRepo)
			request, owner, eventID := archiveFixtureRequest(f.borrowingArchiveFixture), f.owner, f.transactionEvent
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
			queries := events.Queries{}
			require.NoError(t, queries.PrepareConsumerSignal(extCtx, publisher, events.PrepareConsumerSignalParams{
				EventID: eventID, Signal: string(events.SignalNoticeCreated),
			}))
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
			}, 3*time.Second, 10*time.Millisecond, "archiver must reach the event lock after seeing no committed consumer work")
			require.NoError(t, publisher.Commit(extCtx))
			require.NoError(t, <-result)
			require.False(t, stored, "final check must observe the newly committed signal")
		})
	}
}

func TestLendingArchiveIgnoresOtherConsumerWork(t *testing.T) {
	f := createLendingArchiveFixture(t)
	repo := prRepo.(*pr_db.PgPrRepo)
	queries := events.Queries{}
	_, err := repo.Pool.Exec(appCtx, `UPDATE event SET last_signal=$2 WHERE id=$1`, f.transactionEvent, events.SignalTaskComplete)
	require.NoError(t, err)
	require.NoError(t, queries.PrepareConsumerSignal(appCtx, repo.Pool, events.PrepareConsumerSignalParams{
		EventID: f.transactionEvent, Signal: string(events.SignalTaskComplete),
	}))
	stored, err := repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.True(t, stored)
	var count int
	require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT COUNT(*) FROM event_consumer_work WHERE event_id=$1`, f.transactionEvent).Scan(&count))
	require.Equal(t, 1, count, "lending archival must preserve transaction consumer work")
}
