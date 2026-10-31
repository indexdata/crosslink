package test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBibItemScopeNamePersistence(t *testing.T) {
	resetDb()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	res, body := jsonReq(t, http.MethodPost, "/entries", `{"name":"NCIP scope","type":"Institution","lmsConfig":{"bibItemScopeName":"Title"}}`, headers)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var created struct{ Id string }
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	path := "/entries/by-id/" + created.Id
	check := func(want *string) {
		t.Helper()
		res, body := jsonReq(t, http.MethodGet, path, "", headers)
		require.Equal(t, http.StatusOK, res.StatusCode, body)
		var saved struct{ LmsConfig map[string]any }
		require.NoError(t, json.Unmarshal([]byte(body), &saved))
		if want == nil {
			require.NotContains(t, saved.LmsConfig, "bibItemScopeName")
		} else {
			require.Equal(t, *want, saved.LmsConfig["bibItemScopeName"])
		}
	}
	check(new("Title"))
	for _, tc := range []struct {
		patch string
		want  *string
	}{
		{`{"lmsConfig":{"requestItemRequestScopeType":"auto"}}`, new("Title")},
		{`{"lmsConfig":{"bibItemScopeName":"Record"}}`, new("Record")},
		{`{"lmsConfig":{"bibItemScopeName":null}}`, nil},
	} {
		res, body = jsonReq(t, http.MethodPatch, path, tc.patch, headers)
		require.Equal(t, http.StatusNoContent, res.StatusCode, body)
		check(tc.want)
	}
}

func TestImportBibItemScopeName(t *testing.T) {
	resetImportState(t)
	key := symbolObject("ISIL", "SCOPE")
	record := entryImportRecord(key, "Scope", nil, "Consortium")
	lms := record["data"].(map[string]any)["lmsConfig"].(map[string]any)
	for _, value := range []any{"Title", "Record", nil} {
		lms["bibItemScopeName"] = value
		res, result := importRequest(t, []any{record}, "update", standardHeaders)
		require.Equal(t, http.StatusOK, res.StatusCode)
		require.Empty(t, result.Errors)
		res, body := jsonReq(t, http.MethodGet, "/entries/by-id/"+importEntryUUID(key).String(), "", standardHeaders)
		require.Equal(t, http.StatusOK, res.StatusCode, body)
		var saved struct{ LmsConfig map[string]any }
		require.NoError(t, json.Unmarshal([]byte(body), &saved))
		if value == nil {
			require.NotContains(t, saved.LmsConfig, "bibItemScopeName")
		} else {
			require.Equal(t, value, saved.LmsConfig["bibItemScopeName"])
		}
	}
}

func TestBibItemScopeNameMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	exec := func(sql string) { t.Helper(); _, err := tx.Exec(ctx, sql); require.NoError(t, err) }
	exec("CREATE SCHEMA bib_item_scope_migration_test; SET LOCAL search_path TO bib_item_scope_migration_test")
	exec("CREATE TABLE lms_configs (request_item_scope_type text); INSERT INTO lms_configs VALUES ('Item')")
	up, err := os.ReadFile("../migrations/024_bib_item_scope_name.up.sql")
	require.NoError(t, err)
	exec(string(up))
	var scope string
	var name *string
	require.NoError(t, tx.QueryRow(ctx, "SELECT request_item_scope_type, bib_item_scope_name FROM lms_configs").Scan(&scope, &name))
	require.Equal(t, "Item", scope)
	require.Nil(t, name)
	down, err := os.ReadFile("../migrations/024_bib_item_scope_name.down.sql")
	require.NoError(t, err)
	exec(string(down))
	require.NoError(t, tx.QueryRow(ctx, "SELECT request_item_scope_type FROM lms_configs").Scan(&scope))
	require.Equal(t, "Item", scope)
}
