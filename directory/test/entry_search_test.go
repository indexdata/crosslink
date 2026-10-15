package test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func searchDirectoryEntries(t *testing.T, endpoint, query string, headers map[string]string) ([]uuid.UUID, int) {
	t.Helper()
	response, body := jsonReq(t, http.MethodGet, endpoint+"&cql="+url.QueryEscape(query), "", headers)
	require.Equal(t, http.StatusOK, response.StatusCode, body)
	var result struct {
		Items []struct{ ID uuid.UUID } `json:"items"`
		About struct{ Count int }      `json:"about"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &result))
	var ids []uuid.UUID
	for _, entry := range result.Items {
		ids = append(ids, entry.ID)
	}
	return ids, result.About.Count
}

func requireDirectorySearch(t *testing.T, query string, want ...uuid.UUID) {
	t.Helper()
	ids, count := searchDirectoryEntries(t, "/entries?limit=100", query,
		map[string]string{"X-Okapi-Permissions": `["directory.system.all"]`})
	require.Equal(t, len(want), count)
	require.ElementsMatch(t, want, ids)
}

func TestEntryServerChoice(t *testing.T) {
	resetDb()
	ctx := context.Background()
	id, otherID := uuid.New(), uuid.New()
	_, err := dbpool.Exec(ctx, `INSERT INTO entries
		(id, name, description, organization_id, email, phone_number, type, tenant, contact_name)
		VALUES ($1, 'The Atlas Library', 'Regional research', 'ORG123', 'desk@example.test',
		'5551234567', 'Institution', 'ANINST', 'NeverIndexed'),
		($2, 'Z Atlas', NULL, NULL, NULL, NULL, 'Institution', 'OTHER', NULL)`, id, otherID)
	require.NoError(t, err)
	_, err = dbpool.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'ISIL', 'US-ATLAS')`, id)
	require.NoError(t, err)

	for _, tc := range []struct {
		query string
		ids   []uuid.UUID
	}{
		{`cql.serverChoice="ATLAS"`, []uuid.UUID{id, otherID}},
		{`cql.serverChoice="research"`, []uuid.UUID{id}},
		{`cql.serverChoice="org123"`, []uuid.UUID{id}},
		{`cql.serverChoice="DESK@EXAMPLE.TEST"`, []uuid.UUID{id}},
		{`cql.serverChoice="5551234567"`, []uuid.UUID{id}},
		{`cql.serverChoice="isil:us-atlas"`, []uuid.UUID{id}},
		{`cql.serverChoice="us-atlas"`, []uuid.UUID{id}},
		{`cql.serverChoice="atlas research"`, []uuid.UUID{id}},
		{`cql.serverChoice all "atlas research"`, []uuid.UUID{id}},
		{`cql.serverChoice any "research absentword"`, []uuid.UUID{id}},
		{`cql.serverChoice adj "atlas library"`, []uuid.UUID{id}},
		{`cql.serverChoice adj "library atlas"`, nil},
		{`cql.serverChoice="resear*"`, []uuid.UUID{id}},
		{`cql.serverChoice="the"`, []uuid.UUID{id}},
		{`cql.serverChoice="libraries"`, nil},
		{`cql.serverChoice="NeverIndexed"`, nil},
		{`cql.serverChoice="atlas" AND tenant="OTHER"`, []uuid.UUID{otherID}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			requireDirectorySearch(t, tc.query, tc.ids...)
		})
	}

	headers := map[string]string{"X-Okapi-Permissions": `["directory.system.all"]`}
	ids, count := searchDirectoryEntries(t, "/entries?limit=1&offset=1", `cql.serverChoice="atlas"`, headers)
	require.Equal(t, 2, count)
	require.Equal(t, []uuid.UUID{otherID}, ids)
	ids, count = searchDirectoryEntries(t, "/entries/owned?limit=100",
		`cql.serverChoice="atlas" OR tenant="OTHER"`, map[string]string{
			"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.institution.all"]`,
		})
	require.Equal(t, 1, count)
	require.Equal(t, []uuid.UUID{id}, ids)
}

func TestEntryServerChoiceAPIUpdates(t *testing.T) {
	resetDb()
	id := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	path := "/entries/by-id/" + id.String()
	headers := map[string]string{"X-Okapi-Tenant": "ANINST", "X-Okapi-Permissions": `["directory.consortium.all"]`}
	for _, tc := range []struct {
		body string
		term string
	}{
		{`{"name":"SearchName"}`, "searchname"},
		{`{"description":"SearchDescription"}`, "searchdescription"},
		{`{"organizationId":"SearchOrganization"}`, "searchorganization"},
		{`{"email":"SearchEmail@example.test"}`, "searchemail@example.test"},
		{`{"phoneNumber":"5559876543"}`, "5559876543"},
		{`{"symbols":[{"authority":"SEARCHAUTH","symbol":"SEARCHSYMBOL"}]}`, "searchauth:searchsymbol"},
	} {
		response, body := jsonReq(t, http.MethodPatch, path, tc.body, headers)
		require.Equal(t, http.StatusNoContent, response.StatusCode, body)
		requireDirectorySearch(t, `cql.serverChoice="`+tc.term+`"`, id)
	}
	response, body := jsonReq(t, http.MethodPatch, path, `{"description":null,"symbols":[]}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	requireDirectorySearch(t, `cql.serverChoice any "searchdescription searchsymbol"`)
	response, body = jsonReq(t, http.MethodPatch, path, `{"name":"ReplacementName","symbols":[{"authority":"SEARCH","symbol":"CASCADESEARCH"}]}`, headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	requireDirectorySearch(t, `cql.serverChoice="searchname"`)
	requireDirectorySearch(t, `cql.serverChoice="replacementname cascadesearch"`, id)
	response, body = jsonReq(t, http.MethodDelete, path, "", headers)
	require.Equal(t, http.StatusNoContent, response.StatusCode, body)
	requireDirectorySearch(t, `cql.serverChoice any "replacementname cascadesearch"`)
}

func TestEntryServerChoiceImportUpdates(t *testing.T) {
	resetImportState(t)
	key := symbolObject("ISIL", "SEARCHIMPORT")
	record := entryImportRecord(key, "ImportedSearch", nil, "Consortium")
	response, result := importRequest(t, []any{record}, "fail", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Empty(t, result.Errors)
	id := importEntryUUID(key)
	requireDirectorySearch(t, `cql.serverChoice="importedsearch searchimport"`, id)
	data := record["data"].(map[string]any)
	data["name"] = "ReplacedSearch"
	data["symbols"] = []any{symbolObject("ISIL", "NEWSEARCHIMPORT")}
	response, result = importRequest(t, []any{record}, "update", standardHeaders)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Empty(t, result.Errors)
	requireDirectorySearch(t, `cql.serverChoice="replacedsearch newsearchimport"`, id)
	requireDirectorySearch(t, `cql.serverChoice any "importedsearch searchimport"`)
}

func TestEntryServerChoiceSymbolChanges(t *testing.T) {
	resetDb()
	ctx := context.Background()
	owner := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	symbolID := uuid.New()
	_, err := dbpool.Exec(ctx, `INSERT INTO symbols (id, owner, authority, symbol) VALUES ($1, $2, 'OLDSEARCHAUTH', 'ORIGINALSEARCH')`, symbolID, owner)
	require.NoError(t, err)
	requireDirectorySearch(t, `cql.serverChoice="oldsearchauth:originalsearch"`, owner)
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT id FROM entries WHERE id=$1 FOR UPDATE`, owner)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `UPDATE symbols SET authority='NEWSEARCHAUTH', symbol='UPDATEDSEARCH' WHERE id=$1`, symbolID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	requireDirectorySearch(t, `cql.serverChoice any "oldsearchauth originalsearch"`)
	requireDirectorySearch(t, `cql.serverChoice="newsearchauth:updatedsearch"`, owner)

	tx, err = dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE entries SET name='RolledBackSearch' WHERE id=$1`, owner)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM symbols WHERE id=$1`, symbolID)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	requireDirectorySearch(t, `cql.serverChoice="rolledbacksearch"`)
	requireDirectorySearch(t, `cql.serverChoice="updatedsearch"`, owner)
	tx, err = dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `SELECT id FROM entries WHERE id=$1 FOR UPDATE`, owner)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `DELETE FROM symbols WHERE id=$1`, symbolID)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(ctx))
	requireDirectorySearch(t, `cql.serverChoice="updatedsearch"`)
}

func TestEntryServerChoiceConcurrentUpdates(t *testing.T) {
	resetDb()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'SEARCH', 'FIRSTCONCURRENT')`, owner)
	require.NoError(t, err)
	// Both writes start before the first transaction commits. They must rebuild
	// from the latest committed symbols after acquiring the entry lock.
	symbolConn, err := dbpool.Acquire(ctx)
	require.NoError(t, err)
	defer symbolConn.Release()
	entryConn, err := dbpool.Acquire(ctx)
	require.NoError(t, err)
	defer entryConn.Release()
	finished := make(chan error, 2)
	go func() {
		_, err := symbolConn.Exec(ctx, `INSERT INTO symbols (owner, authority, symbol) VALUES ($1, 'SEARCH', 'SECONDCONCURRENT')`, owner)
		finished <- err
	}()
	go func() {
		_, err := entryConn.Exec(ctx, `UPDATE entries SET description='ConcurrentDescription' WHERE id=$1`, owner)
		finished <- err
	}()
	pids := []int32{int32(symbolConn.Conn().PgConn().PID()), int32(entryConn.Conn().PgConn().PID())}
	require.Eventually(t, func() bool {
		var waiting int
		err := dbpool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE pid=ANY($1) AND wait_event_type='Lock'`, pids).Scan(&waiting)
		return err == nil && waiting == 2
	}, 5*time.Second, 10*time.Millisecond, "both writers must wait for the entry lock")
	require.NoError(t, tx.Commit(ctx))
	for range 2 {
		require.NoError(t, <-finished)
	}
	requireDirectorySearch(t, `cql.serverChoice="firstconcurrent secondconcurrent concurrentdescription"`, owner)
}
