package db

import (
	"os"
	"testing"
	"time"

	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	"github.com/stretchr/testify/require"
)

func TestArchiveRequestReportsChangedSelection(t *testing.T) {
	for _, side := range []string{"borrowing", "lending"} {
		for _, change := range []string{"state", "timestamp", "removed"} {
			t.Run(side+"/"+change, func(t *testing.T) {
				f := createLendingArchiveFixture(t)
				repo := prRepo.(*pr_db.PgPrRepo)
				request, owner := archiveFixtureRequest(f.borrowingArchiveFixture), f.owner
				archive := repo.ArchiveBorrowingRequest
				if side == "lending" {
					request, owner, archive = lendingFixtureRequest(f), f.supplierOwner, repo.ArchiveLendingRequest
				}
				var err error
				switch change {
				case "state":
					_, err = repo.Pool.Exec(appCtx, `UPDATE patron_request SET state='NEW' WHERE id=$1`, request.ID)
				case "timestamp":
					_, err = repo.Pool.Exec(appCtx, `UPDATE patron_request SET updated_at=updated_at+interval '1 second' WHERE id=$1`, request.ID)
				case "removed":
					_, err = repo.Pool.Exec(appCtx, `DELETE FROM patron_request WHERE id=$1`, request.ID)
				}
				require.NoError(t, err)
				result, err := archive(appCtx, request, owner, time.Now(), false)
				require.NoError(t, err)
				require.Equal(t, pr_db.ArchiveResultChangedOrBusy, result)
			})
		}
	}
}

func TestArchiveRequestBooleanCompatibility(t *testing.T) {
	checkArchiveRequestBooleanCompatibility(t)
}

func checkArchiveRequestBooleanCompatibility(t *testing.T) {
	t.Helper()
	for _, side := range []string{"borrowing", "lending"} {
		t.Run(side, func(t *testing.T) {
			f := createLendingArchiveFixture(t)
			repo := prRepo.(*pr_db.PgPrRepo)
			request, owner, eventID := archiveFixtureRequest(f.borrowingArchiveFixture), f.owner, f.borrowerEvent
			query := `SELECT archive_borrowing_request($1,$2,$3,$4,$5,$6)`
			if side == "lending" {
				request, owner, eventID = lendingFixtureRequest(f), f.supplierOwner, f.lendingEvent
				query = `SELECT archive_lending_request($1,$2,$3,$4,$5,$6)`
			}
			_, err := repo.Pool.Exec(appCtx, `UPDATE event SET event_status='PROCESSING' WHERE id=$1`, eventID)
			require.NoError(t, err)
			var archived bool
			require.NoError(t, repo.Pool.QueryRow(appCtx, query, request.ID, owner, string(request.State), request.UpdatedAt.Time, time.Now(), false).Scan(&archived))
			require.False(t, archived)
			_, err = repo.Pool.Exec(appCtx, `UPDATE event SET event_status='SUCCESS' WHERE id=$1`, eventID)
			require.NoError(t, err)
			require.NoError(t, repo.Pool.QueryRow(appCtx, query, request.ID, owner, string(request.State), request.UpdatedAt.Time, time.Now(), false).Scan(&archived))
			require.True(t, archived)
		})
	}
}

func TestArchiveSkipReasonsMigrationRoundTrip(t *testing.T) {
	repo := prRepo.(*pr_db.PgPrRepo)
	down, err := os.ReadFile("../../../migrations/081_archive_skip_reasons.down.sql")
	require.NoError(t, err)
	up, err := os.ReadFile("../../../migrations/081_archive_skip_reasons.up.sql")
	require.NoError(t, err)
	f := createLendingArchiveFixture(t)
	result, err := repo.ArchiveLendingRequest(appCtx, lendingFixtureRequest(f), f.supplierOwner, time.Now(), false)
	require.NoError(t, err)
	require.Equal(t, pr_db.ArchiveResultArchived, result)
	_, err = repo.Pool.Exec(appCtx, string(down))
	require.NoError(t, err)
	// Restore the current schema even if an assertion fails while rolled back.
	t.Cleanup(func() {
		_, err := repo.Pool.Exec(appCtx, string(up))
		require.NoError(t, err)
	})
	for _, side := range []string{"borrowing", "lending"} {
		var detailed bool
		require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT to_regprocedure($1) IS NOT NULL`, "archive_"+side+"_request_result(text,text,text,timestamp,timestamp,boolean)").Scan(&detailed))
		require.False(t, detailed)
	}
	// The original boolean API still archives requests after rollback.
	checkArchiveRequestBooleanCompatibility(t)
	var archived int
	require.NoError(t, repo.Pool.QueryRow(appCtx, `SELECT count(*) FROM archived_resource WHERE resource_id=$1`, f.lending).Scan(&archived))
	require.Equal(t, 1, archived, "rollback must preserve existing archives")
}
