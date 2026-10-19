package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEntryCQLPickupLocation(t *testing.T) {
	resetDb()
	ctx := context.Background()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	for i, id := range ids {
		_, err := dbpool.Exec(ctx, "INSERT INTO entries (id, name, type, tenant) VALUES ($1, $2, 'Institution', 'pickup-cql')", id, fmt.Sprintf("Location %d", i))
		require.NoError(t, err)
	}
	// Cover enabled, disabled, NULL, and absent ILL configurations.
	_, err := dbpool.Exec(ctx, "INSERT INTO ill_configs (entry, is_pickup_location) VALUES ($1, TRUE), ($2, FALSE), ($3, NULL)", ids[0], ids[1], ids[2])
	require.NoError(t, err)
	// A pickup code must not override an explicitly disabled flag.
	_, err = dbpool.Exec(ctx, "INSERT INTO lms_configs (entry, address, from_agency, requester_pickup_location) VALUES ($1, 'unused', 'MAIN', 'main')", ids[1])
	require.NoError(t, err)
	headers := map[string]string{"X-Okapi-Permissions": `["directory.system.all"]`}
	for _, tc := range []struct {
		query string
		ids   []uuid.UUID
		count int
	}{
		{`isPickupLocation=true`, ids[:1], 1},
		{`isPickupLocation=false`, ids[1:], 3},
		{`isPickupLocation<>true`, ids[1:], 3},
	} {
		t.Run(tc.query, func(t *testing.T) {
			query := url.QueryEscape(`tenant="pickup-cql" AND ` + tc.query)
			response, body := jsonReq(t, http.MethodGet, "/entries?cql="+query, "", headers)
			require.Equal(t, http.StatusOK, response.StatusCode, body)
			var result struct {
				Items []struct{ ID uuid.UUID } `json:"items"`
				About struct{ Count int }      `json:"about"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &result))
			require.Equal(t, tc.count, result.About.Count)
			var actual []uuid.UUID
			for _, item := range result.Items {
				actual = append(actual, item.ID)
			}
			require.Equal(t, tc.ids, actual)
		})
	}
}

func TestPickupLocationFlagPersistence(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	path := "/entries/by-id/00000000-0000-0000-0000-000000000002"
	for _, enabled := range []bool{true, false} {
		response, body := jsonReq(t, http.MethodPatch, path, fmt.Sprintf(`{"illConfig":{"isPickupLocation":%t}}`, enabled), headers)
		require.Equal(t, http.StatusNoContent, response.StatusCode, body)
		// An unrelated patch must preserve the explicit flag, including false.
		response, body = jsonReq(t, http.MethodPatch, path, `{"illConfig":{"includeSupplierInfo":true}}`, headers)
		require.Equal(t, http.StatusNoContent, response.StatusCode, body)
		response, body = jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, response.StatusCode, body)
		var entry struct {
			IllConfig struct {
				IsPickupLocation *bool `json:"isPickupLocation"`
			} `json:"illConfig"`
		}
		require.NoError(t, json.Unmarshal([]byte(body), &entry))
		require.NotNil(t, entry.IllConfig.IsPickupLocation)
		require.Equal(t, enabled, *entry.IllConfig.IsPickupLocation)
	}
}

func TestPickupLocationFlagMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(query string, args ...any) {
		t.Helper()
		_, err := tx.Exec(ctx, query, args...)
		require.NoError(t, err)
	}
	exec("CREATE SCHEMA pickup_flag_migration_test; SET LOCAL search_path TO pickup_flag_migration_test")
	apply := func(version int) {
		t.Helper()
		paths, err := filepath.Glob(fmt.Sprintf("../migrations/%03d_*.up.sql", version))
		require.NoError(t, err)
		require.Len(t, paths, 1)
		sql, err := os.ReadFile(paths[0])
		require.NoError(t, err)
		exec(string(sql))
	}
	for version := 1; version <= 11; version++ {
		apply(version)
	}
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	for _, id := range ids {
		exec("INSERT INTO entries (id, name, type) VALUES ($1, 'Pickup migration test', 'Institution')", id)
	}
	exec("INSERT INTO lms_configs (entry, address, from_agency, requester_pickup_location) VALUES ($1, 'unused', 'MAIN', 'main'), ($2, 'unused', 'MAIN', ''), ($3, 'unused', 'MAIN', NULL)", ids[0], ids[1], ids[2])
	exec("INSERT INTO ill_configs (entry, note_field_separator) VALUES ($1, 'preserved')", ids[0])
	apply(12)
	for i, id := range ids {
		var enabled bool
		require.NoError(t, tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM ill_configs WHERE entry=$1 AND is_pickup_location)", id).Scan(&enabled))
		require.Equal(t, i < 2, enabled)
	}
	var separator string
	require.NoError(t, tx.QueryRow(ctx, "SELECT note_field_separator FROM ill_configs WHERE entry=$1", ids[0]).Scan(&separator))
	require.Equal(t, "preserved", separator)
	down, err := os.ReadFile("../migrations/012_pickup_location.down.sql")
	require.NoError(t, err)
	exec(string(down))
}
