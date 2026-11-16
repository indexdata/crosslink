package prservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/adapter"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/dbutil"
	"github.com/indexdata/crosslink/broker/events"
	"github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	dirapi "github.com/indexdata/crosslink/directory/api"
	"github.com/indexdata/crosslink/testutil"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type localFillPickupBarrier struct {
	ill_db.IllRepo
	reached, release chan struct{}
}

func (r *localFillPickupBarrier) GetCachedPeerByDirectoryEntryID(ctx common.ExtendedContext, id uuid.UUID, _ adapter.DirectoryLookupAdapter) (ill_db.Peer, string, error) {
	close(r.reached)
	select {
	case <-r.release:
	case <-ctx.Done():
		return ill_db.Peer{}, "", ctx.Err()
	}
	return ill_db.Peer{CustomData: dirapi.Entry{Id: &id, Symbols: pickupSymbols("REQ"), LmsConfig: &dirapi.LmsConfig{RequesterPickupLocation: new("pickup")}}}, "", nil
}

// Exercise real PostgreSQL snapshot locks while Directory I/O is blocked.
// Every mutation completes before pickup is released, proving no DB lock is held
// across that I/O. Channels determine ordering; sleeps are not used.
func TestLocalFillRevalidatesAfterPickup(t *testing.T) {
	base, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ctx := common.CreateExtCtxWithArgs(base, nil)
	pgc, err := testutil.RunPostgres(base)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pgc.Terminate(context.Background())) })
	conn, err := pgc.ConnectionString(base, "sslmode=disable")
	require.NoError(t, err)
	conn += dbutil.SearchPath("crosslink_broker")
	require.NoError(t, dbutil.RunDbProvision(conn, "crosslink_broker"))
	_, _, _, err = dbutil.RunDbMigrations("file://../../migrations", conn)
	require.NoError(t, err)
	repo := new(ill_db.PgIllRepo)
	repo.Pool, err = dbutil.InitDbPool(conn)
	require.NoError(t, err)
	t.Cleanup(repo.Pool.Close)

	for _, change := range []string{"readiness revoked", "different item", "different bib", "same item new token", "attempt deleted", "legacy gets token", "supplier skipped", "unchanged", "legacy unchanged"} {
		t.Run(change, func(t *testing.T) {
			pr := pr_db.PatronRequest{ID: uuid.NewString(), RequesterSymbol: getDbText("ISIL:REQ"), RequesterPickupLocationID: pgtype.UUID{Bytes: uuid.New(), Valid: true}}
			peer, err := repo.SavePeer(ctx, ill_db.SavePeerParams{ID: uuid.NewString(), Name: "local", RefreshPolicy: ill_db.RefreshPolicyNever, RefreshTime: pgtype.Timestamp{Time: time.Now(), Valid: true}})
			require.NoError(t, err)
			trans, err := repo.SaveIllTransaction(ctx, ill_db.SaveIllTransactionParams{ID: uuid.NewString(), RequesterRequestID: getDbText(pr.ID), RequesterSymbol: pr.RequesterSymbol, Timestamp: pgtype.Timestamp{Time: time.Now(), Valid: true}})
			require.NoError(t, err)
			supplier, err := repo.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams{ID: uuid.NewString(), IllTransactionID: trans.ID, SupplierID: peer.ID, SupplierSymbol: pr.RequesterSymbol.String, SupplierStatus: ill_db.SupplierStateSelectedPg, LocalSupplier: true, LocalID: getDbText("bib"), ItemID: getDbText("copy"), HoldingsPolicyResolved: pgtype.Bool{Bool: true, Valid: true}})
			require.NoError(t, err)
			if change != "legacy gets token" && change != "legacy unchanged" {
				require.NoError(t, repo.SaveAvailabilityAttempt(ctx, ill_db.SaveAvailabilityAttemptParams{IllTransactionID: trans.ID, LocatedSupplierID: supplier.ID, Token: "original"}))
			}
			barrier := &localFillPickupBarrier{IllRepo: repo, reached: make(chan struct{}), release: make(chan struct{})}
			service := &PatronRequestActionService{illRepo: barrier}
			lmsAdapter := new(mockLmsAdapter)
			unchanged := change == "unchanged" || change == "legacy unchanged"
			if unchanged {
				// Stop after verifying dispatch; no message sender is required.
				lmsAdapter.On("RequestItem", mock.Anything, mock.Anything, mock.Anything, "pickup", mock.Anything).Return(nil, errors.New("dispatch reached")).Once()
			}
			done := make(chan actionExecutionResult, 1)
			go func() { done <- service.fillLocallyBorrowingRequest(ctx, "", pr, lmsAdapter, actionParams{}) }()
			select {
			case <-barrier.reached:
			case <-base.Done():
				t.Fatal("pickup lookup was not reached")
			}
			require.NoError(t, repo.WithTxFunc(ctx, func(tx ill_db.IllRepo) error {
				if _, err := tx.GetIllTransactionByIdForUpdate(ctx, trans.ID); err != nil {
					return err
				}
				switch change {
				case "readiness revoked":
					supplier.HoldingsPolicyResolved.Bool = false
				case "different item":
					supplier.ItemID = getDbText("replacement")
				case "different bib":
					supplier.LocalID = getDbText("other-bib")
				case "supplier skipped":
					supplier.SupplierStatus = ill_db.SupplierStateSkippedPg
				case "same item new token", "legacy gets token":
					return tx.SaveAvailabilityAttempt(ctx, ill_db.SaveAvailabilityAttemptParams{IllTransactionID: trans.ID, LocatedSupplierID: supplier.ID, Token: "new"})
				case "attempt deleted":
					return tx.DeleteAvailabilityAttempt(ctx, trans.ID)
				}
				_, err := tx.SaveLocatedSupplier(ctx, ill_db.SaveLocatedSupplierParams(supplier))
				return err
			}))
			close(barrier.release)
			select {
			case result := <-done:
				require.Equal(t, events.EventStatusError, result.status)
				if unchanged {
					require.Equal(t, "LMS RequestItem failed", result.result.EventError.Message)
					lmsAdapter.AssertExpectations(t)
				} else {
					require.Equal(t, "Local supplier selection changed", result.result.EventError.Message)
					lmsAdapter.AssertNotCalled(t, "RequestItem", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything)
				}
			case <-base.Done():
				t.Fatal("local fill did not complete")
			}
		})
	}
}
