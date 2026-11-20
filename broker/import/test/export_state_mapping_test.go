package test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportAwaitPickingStateMapping(t *testing.T) {
	source, err := os.ReadFile("../../../migration/export-crosslink-open-patron-requests.sql")
	require.NoError(t, err)
	_, setup, found := strings.Cut(string(source), "CREATE TEMP TABLE crosslink_state_map")
	require.True(t, found)
	setup, _, found = strings.Cut(setup, "-- ISO18626 CopyrightCompliance codes.")
	require.True(t, found)
	ctx := context.Background()
	tx, err := importTestPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()
	_, err = tx.Exec(ctx, "CREATE TEMP TABLE crosslink_state_map"+setup)
	require.NoError(t, err)
	for _, tc := range []struct{ state, service, want string }{
		{"RES_AWAIT_PICKING", "Loan", "SEARCHING"},
		{"RES_AWAIT_PICKING", "CopyOrLoan", "SEARCHING"},
		{"RES_AWAIT_PICKING", "Copy", "WILL_SUPPLY"},
		{"RES_NEW_AWAIT_PULL_SLIP", "Loan", "WILL_SUPPLY"},
		{"RES_ERROR", "Loan", "ITEM_PENDING"},
	} {
		var state string
		require.NoError(t, tx.QueryRow(ctx, `SELECT crosslink_state FROM crosslink_state_map
            WHERE legacy_state=$1 AND side='lending' AND service_type IN ($2, '*')
            ORDER BY service_type = $2 DESC LIMIT 1`, tc.state, tc.service).Scan(&state))
		assert.Equal(t, tc.want, state, tc.state+"/"+tc.service)
	}
}
