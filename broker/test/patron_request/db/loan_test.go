package db

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoanDateAndItemProgressPersistence(t *testing.T) {
	id, itemID := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Truncate(time.Microsecond)
	due := now.Add(-time.Hour)
	pr, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
		ID: id, Side: prservice.SideLending, State: prservice.LenderStateReceived,
		CreatedAt: pgtype.Timestamp{Time: now, Valid: true}, UpdatedAt: pgtype.Timestamp{Time: now, Valid: true},
		DueAt: pgtype.Timestamptz{Time: due, Valid: true}, Language: "english", Items: []pr_db.PrItem{},
		IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}},
	})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, prRepo.DeletePatronRequest(appCtx, id)) })
	item, err := prRepo.SaveItem(appCtx, pr_db.SaveItemParams{ID: itemID, PrID: id, Barcode: "barcode", CreatedAt: pgtype.Timestamp{Time: now, Valid: true}})
	require.NoError(t, err)
	assert.Equal(t, pr_db.LmsStatusUnknown, item.LmsStatus)
	require.NoError(t, prRepo.SetItemLmsStatus(appCtx, pr_db.SetItemLmsStatusParams{ID: itemID, LmsStatus: pr_db.LmsStatusCheckedOut, LmsDueDate: pr.DueAt}))
	// Metadata edits must not erase a confirmed LMS outcome.
	_, err = prRepo.SaveItem(appCtx, pr_db.SaveItemParams{ID: itemID, PrID: id, Barcode: "barcode", CreatedAt: item.CreatedAt, Title: pgtype.Text{String: "Updated title", Valid: true}})
	require.NoError(t, err)
	view, err := prRepo.GetPatronRequestSearchView(appCtx, id)
	require.NoError(t, err)
	assert.True(t, view.DueAt.Time.Equal(due))
	require.Len(t, view.Items, 1)
	assert.Equal(t, pr_db.LmsStatusCheckedOut, view.Items[0].LmsStatus)
	require.NotNil(t, view.Items[0].LmsDueDate)
	assert.True(t, view.Items[0].LmsDueDate.Equal(due))
	query, err := pr_db.ParsePatronRequestsCql(`id = "` + id + `" and due_at < "` + now.Format(time.RFC3339Nano) + `"`)
	require.NoError(t, err)
	requests, _, err := prRepo.ListPatronRequests(appCtx, pr_db.ListPatronRequestsParams{Limit: 10}, query)
	require.NoError(t, err)
	require.Len(t, requests, 1)
	assert.True(t, requests[0].DueAt.Time.Equal(due))
	assert.Error(t, prRepo.SetItemLmsStatus(appCtx, pr_db.SetItemLmsStatusParams{ID: itemID, LmsStatus: "SKIPPED"}))
}

func TestLoanTransactionReturnsCommitFailure(t *testing.T) {
	// A deferred constraint fails only at commit, not inside the callback.
	err := prRepo.WithTxFunc(appCtx, func(repo pr_db.PrRepo) error {
		conn := repo.(*pr_db.PgPrRepo).GetConnOrTx()
		_, err := conn.Exec(appCtx, `CREATE TEMP TABLE loan_commit_test (id int UNIQUE DEFERRABLE INITIALLY DEFERRED) ON COMMIT DROP`)
		if err != nil {
			return err
		}
		_, err = conn.Exec(appCtx, `INSERT INTO loan_commit_test VALUES (1), (1)`)
		return err
	})
	require.ErrorContains(t, err, "duplicate key")
}

func TestLoanMigrationBackfillsValidDates(t *testing.T) {
	down, err := os.ReadFile("../../../migrations/065_loan_due_dates.down.sql")
	require.NoError(t, err)
	up, err := os.ReadFile("../../../migrations/065_loan_due_dates.up.sql")
	require.NoError(t, err)
	wants := make(map[string]string)
	now := pgtype.Timestamp{Time: time.Now().UTC(), Valid: true}
	for _, tc := range []struct {
		date string
		want string
	}{
		{`"2026-10-01T23:59:59+02:00"`, "2026-10-01T21:59:59Z"},
		{`"2020-01-01T00:00:00Z"`, "2020-01-01T00:00:00Z"},
		{`null`, ""},
		{`""`, ""},
		{`"invalid"`, ""},
		{`"2026-02-30T00:00:00Z"`, ""},
		{`"0000-01-01T00:00:00Z"`, ""},
		{`"0001-01-01T00:00:00Z"`, ""},
		{`"2026-10-01"`, ""},
		{`"2026-10-01T23:59:59"`, ""},
		{`"infinity"`, ""},
		{`{}`, ""},
	} {
		id := uuid.NewString()
		_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
			ID: id, Side: prservice.SideLending, State: prservice.LenderStateReceived,
			CreatedAt: now, UpdatedAt: now, Language: "english", Items: []pr_db.PrItem{},
			IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}},
		})
		require.NoError(t, err)
		t.Cleanup(func() { assert.NoError(t, prRepo.DeletePatronRequest(appCtx, id)) })
		_, err = prRepo.(*pr_db.PgPrRepo).GetConnOrTx().Exec(appCtx,
			`UPDATE patron_request SET ill_response = jsonb_build_object('statusInfo', jsonb_build_object('dueDate', $2::jsonb)) WHERE id = $1`, id, tc.date)
		require.NoError(t, err)
		wants[id] = tc.want
	}
	// Exercise the actual migration without changing the shared test schema.
	restore := errors.New("restore current schema")
	got := make(map[string]string)
	err = prRepo.WithTxFunc(appCtx, func(repo pr_db.PrRepo) error {
		conn := repo.(*pr_db.PgPrRepo).GetConnOrTx()
		if _, err := conn.Exec(appCtx, string(down)); err != nil {
			return err
		}
		if _, err := conn.Exec(appCtx, string(up)); err != nil {
			return err
		}
		for id := range wants {
			var due pgtype.Timestamptz
			if err := conn.QueryRow(appCtx, `SELECT due_at FROM patron_request WHERE id = $1`, id).Scan(&due); err != nil {
				return err
			}
			got[id] = ""
			if due.Valid {
				got[id] = due.Time.UTC().Format(time.RFC3339)
			}
		}
		return restore
	})
	require.ErrorIs(t, err, restore)
	assert.Equal(t, wants, got)
}

func TestLoanRollbackRestoresSupportedStates(t *testing.T) {
	down, err := os.ReadFile("../../../migrations/065_loan_due_dates.down.sql")
	require.NoError(t, err)
	ids := make(map[string]string)
	now := pgtype.Timestamp{Time: time.Now().UTC(), Valid: true}
	for _, side := range []pr_db.PatronRequestSide{prservice.SideBorrowing, prservice.SideLending} {
		for _, state := range []pr_db.PatronRequestState{"OVERDUE", "RENEWED", "RENEWAL_PENDING", "SHIPPED"} {
			id := uuid.NewString()
			_, err := prRepo.CreatePatronRequest(appCtx, pr_db.CreatePatronRequestParams{
				ID: id, Side: side, State: state, CreatedAt: now, UpdatedAt: now, Language: "english", Items: []pr_db.PrItem{},
				IllRequest: iso18626.Request{ServiceInfo: &iso18626.ServiceInfo{ServiceType: iso18626.TypeServiceTypeLoan}},
			})
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, prRepo.DeletePatronRequest(appCtx, id)) })
			want := "RECEIVED"
			if state == "SHIPPED" {
				want = "SHIPPED"
			}
			ids[id] = want
		}
	}
	// Execute the actual down migration transactionally, then roll it back so
	// other integration tests retain the current schema and scheduled task.
	restore := errors.New("restore current schema")
	states := make(map[string]string)
	err = prRepo.WithTxFunc(appCtx, func(repo pr_db.PrRepo) error {
		conn := repo.(*pr_db.PgPrRepo).GetConnOrTx()
		if _, err := conn.Exec(appCtx, string(down)); err != nil {
			return err
		}
		for id := range ids {
			var state string
			if err := conn.QueryRow(appCtx, `SELECT state FROM patron_request WHERE id = $1`, id).Scan(&state); err != nil {
				return err
			}
			states[id] = state
		}
		return restore
	})
	require.ErrorIs(t, err, restore)
	assert.Equal(t, ids, states)
}
