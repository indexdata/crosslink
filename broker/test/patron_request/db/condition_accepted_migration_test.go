package db

import (
	"os"
	"testing"
	"time"

	"github.com/indexdata/crosslink/broker/app"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveConditionAccepted(t *testing.T) {
	migration, err := os.ReadFile("../../../migrations/067_remove_condition_accepted.up.sql")
	require.NoError(t, err)
	conn, err := pgx.Connect(appCtx, app.ConnectionString)
	require.NoError(t, err)
	defer func() { assert.NoError(t, conn.Close(appCtx)) }()
	tx, err := conn.Begin(appCtx)
	require.NoError(t, err)
	defer func() { assert.NoError(t, tx.Rollback(appCtx)) }()
	_, err = tx.Exec(appCtx, `CREATE TEMP TABLE patron_request (LIKE public.patron_request INCLUDING DEFAULTS) ON COMMIT DROP`)
	require.NoError(t, err)
	before := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct{ id, side, model, state, want string }{
		{"default", "lending", "default", "CONDITION_ACCEPTED", "WILL_SUPPLY"},
		{"legacy", "lending", "returnables", "CONDITION_ACCEPTED", "WILL_SUPPLY"},
		{"custom", "lending", "custom", "CONDITION_ACCEPTED", "CONDITION_ACCEPTED"},
		{"borrower", "borrowing", "default", "CONDITION_ACCEPTED", "CONDITION_ACCEPTED"},
		{"searching", "lending", "default", "SEARCHING", "SEARCHING"},
	} {
		_, err = tx.Exec(appCtx, `INSERT INTO patron_request (id, side, state_model, state, updated_at) VALUES ($1,$2,$3,$4,$5)`, tc.id, tc.side, tc.model, tc.state, before)
		require.NoError(t, err)
		// Repeated execution must not affect states already migrated.
		for range 2 {
			_, err = tx.Exec(appCtx, string(migration))
			require.NoError(t, err)
		}
		var state string
		var updated time.Time
		require.NoError(t, tx.QueryRow(appCtx, `SELECT state, updated_at FROM patron_request WHERE id=$1`, tc.id).Scan(&state, &updated))
		assert.Equal(t, tc.want, state, tc.id)
		assert.True(t, before.Equal(updated), "migration must preserve aging timestamp")
	}
}
