package db

import (
	"context"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/common"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/stretchr/testify/require"
)

func TestArchiveRequestSkipsLockedItems(t *testing.T) {
	for _, side := range []string{"borrowing", "lending"} {
		for _, active := range []bool{false, true} {
			name := side + "/item-contention"
			if active {
				name = side + "/active-event"
			}
			t.Run(name, func(t *testing.T) {
				f := createLendingArchiveFixture(t)
				repo := prRepo.(*pr_db.PgPrRepo)
				request, owner, eventID := archiveFixtureRequest(f.borrowingArchiveFixture), f.owner, f.transactionEvent
				archive := repo.ArchiveBorrowingRequest
				if side == "lending" {
					request, owner, eventID = lendingFixtureRequest(f), f.supplierOwner, f.lendingEvent
					archive = repo.ArchiveLendingRequest
				}
				if active {
					_, err := repo.Pool.Exec(appCtx, `UPDATE event SET event_status='PROCESSING' WHERE id=$1`, eventID)
					require.NoError(t, err)
				}

				// Hold the item as an updater would, before its trigger locks the parent.
				updater, err := repo.Pool.Begin(appCtx)
				require.NoError(t, err)
				defer func() { _ = updater.Rollback(appCtx) }()
				var itemID string
				require.NoError(t, updater.QueryRow(appCtx, `SELECT id FROM item WHERE pr_id=$1 FOR UPDATE`, request.ID).Scan(&itemID))
				if active {
					// Active events must be checked before waiting even on event locks.
					_, err = updater.Exec(appCtx, `SELECT 1 FROM event WHERE id=$1 FOR UPDATE`, eventID)
					require.NoError(t, err)
				}

				ctx, cancel := context.WithTimeout(appCtx, 5*time.Second)
				defer cancel()
				extCtx := common.CreateExtCtxWithArgs(ctx, nil)
				// Contention must not abort an enclosing workflow transaction.
				err = repo.WithTxFunc(extCtx, func(txRepo pr_db.PrRepo) error {
					tx := txRepo.(*pr_db.PgPrRepo)
					archiveInTx := tx.ArchiveBorrowingRequest
					if side == "lending" {
						archiveInTx = tx.ArchiveLendingRequest
					}
					stored, err := archiveInTx(extCtx, request, owner, time.Now(), false)
					if err != nil {
						return err
					}
					require.False(t, stored)
					_, err = tx.GetConnOrTx().Exec(extCtx, `SELECT 1`)
					return err
				})
				require.NoError(t, err)
				var count int
				require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT COUNT(*) FROM archived_resource WHERE resource_id=$1`, request.ID).Scan(&count))
				require.Zero(t, count)
				require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT COUNT(*) FROM patron_request WHERE id=$1`, request.ID).Scan(&count))
				require.Equal(t, 1, count)

				// The updater can finish, and the candidate can then be retried.
				_, err = updater.Exec(extCtx, `UPDATE item SET barcode='updated' WHERE id=$1`, itemID)
				require.NoError(t, err)
				require.NoError(t, updater.Commit(extCtx))
				if active {
					_, err = repo.Pool.Exec(appCtx, `UPDATE event SET event_status='SUCCESS' WHERE id=$1`, eventID)
					require.NoError(t, err)
				}
				require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT state,updated_at FROM patron_request WHERE id=$1`, request.ID).Scan(&request.State, &request.UpdatedAt))
				stored, err := archive(extCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.True(t, stored)
				var barcode string
				require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT bundle->'items'->0->>'barcode' FROM archived_resource WHERE resource_id=$1`, request.ID).Scan(&barcode))
				require.Equal(t, "updated", barcode)
			})
		}
	}
}
