package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/directory/db"
	"github.com/stretchr/testify/require"
)

func TestEntrySymbolOwnership(t *testing.T) {
	for _, key := range []string{"by-id/00000000-0000-0000-0000-000000000002", "by-symbol/TEST:ANINST"} {
		for _, invalidID := range []struct {
			name string
			id   string
		}{
			{name: "foreign", id: "60000000-0000-0000-0000-000000000003"},
			{name: "missing", id: uuid.NewString()},
		} {
			t.Run(key+"/"+invalidID.name, func(t *testing.T) {
				resetDb()
				ctx := context.Background()
				path := "/entries/" + key
				ownerPath := "/entries/by-id/00000000-0000-0000-0000-000000000002"
				otherPath := "/entries/by-id/00000000-0000-0000-0000-000000000004"
				_, beforeOwner := jsonReq(t, http.MethodGet, ownerPath, "", standardHeaders)
				_, beforeOther := jsonReq(t, http.MethodGet, otherPath, "", standardHeaders)
				var beforeSearch, afterSearch string
				require.NoError(t, dbpool.QueryRow(ctx, `SELECT search::text FROM entries WHERE id='00000000-0000-0000-0000-000000000002'`).Scan(&beforeSearch))

				// Earlier entry, deletion, update, and insertion changes must all
				// roll back when a later symbol ID is rejected.
				body := fmt.Sprintf(`{"name":"RejectedName","symbols":[
					{"id":"60000000-0000-0000-0000-000000000001","authority":"UPDATED","symbol":"RENAMED"},
					{"authority":"NEW","symbol":"CREATED"},
					{"id":%q,"authority":"REJECTED","symbol":"STOLEN"}]}`, invalidID.id)
				response, data := jsonReq(t, http.MethodPatch, path, body, standardHeaders)
				require.Equal(t, http.StatusBadRequest, response.StatusCode, data)
				require.Contains(t, data, "Symbol ID does not belong to this entry")
				_, afterOwner := jsonReq(t, http.MethodGet, ownerPath, "", standardHeaders)
				_, afterOther := jsonReq(t, http.MethodGet, otherPath, "", standardHeaders)
				require.JSONEq(t, beforeOwner, afterOwner)
				require.JSONEq(t, beforeOther, afterOther)
				require.NoError(t, dbpool.QueryRow(ctx, `SELECT search::text FROM entries WHERE id='00000000-0000-0000-0000-000000000002'`).Scan(&afterSearch))
				require.Equal(t, beforeSearch, afterSearch)
				var count int
				require.NoError(t, dbpool.QueryRow(ctx, `SELECT count(*) FROM symbols WHERE id=$1`, invalidID.id).Scan(&count))
				if invalidID.name == "missing" {
					require.Zero(t, count, "a supplied unknown ID must not create a symbol")
				} else {
					require.Equal(t, 1, count)
				}
			})
		}
	}

	t.Run("update owned symbol and create without ID", func(t *testing.T) {
		resetDb()
		owner := uuid.MustParse("00000000-0000-0000-0000-000000000002")
		id := uuid.MustParse("60000000-0000-0000-0000-000000000001")
		response, body := jsonReq(t, http.MethodPatch, "/entries/by-id/"+owner.String(), `{"symbols":[
			{"id":"60000000-0000-0000-0000-000000000001","authority":"updated","symbol":"renamed"},
			{"authority":"new","symbol":"created"}]}`, standardHeaders)
		require.Equal(t, http.StatusNoContent, response.StatusCode, body)
		var count int
		require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT count(*) FROM symbols WHERE owner=$1`, owner).Scan(&count))
		require.Equal(t, 2, count)
		require.NoError(t, dbpool.QueryRow(context.Background(), `SELECT count(*) FROM symbols WHERE id=$1 AND owner=$2 AND authority='UPDATED' AND symbol='RENAMED'`, id, owner).Scan(&count))
		require.Equal(t, 1, count, "updating a symbol must preserve its ID and owner")
		requireDirectorySearch(t, `cql.serverChoice="updated:renamed new:created"`, owner)
		requireDirectorySearch(t, `cql.serverChoice="test:aninst"`)
	})
}

func TestForeignSymbolPatchDoesNotWaitForOwnerLock(t *testing.T) {
	resetDb()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	targetPath := "/entries/by-id/00000000-0000-0000-0000-000000000003"
	_, beforeTarget := jsonReq(t, http.MethodGet, targetPath, "", standardHeaders)
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = db.New(tx).EntryByIdForUpdate(ctx, owner)
	require.NoError(t, err)

	// A PATCH attempting to take this owner's symbol used to lock the symbol
	// and then wait for this entry, deadlocking if this transaction deleted it.
	request := httptest.NewRequest(http.MethodPatch, "/directory"+targetPath,
		strings.NewReader(`{"symbols":[{"id":"60000000-0000-0000-0000-000000000001","authority":"TEST","symbol":"STOLEN"}]}`)).WithContext(ctx)
	request.Header.Set("Content-Type", "application/json")
	for key, value := range standardHeaders {
		request.Header.Set(key, value)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		done <- recorder
	}()
	select {
	case response := <-done:
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	case <-time.After(2 * time.Second):
		t.Fatal("foreign symbol PATCH waited for the owner lock")
	}

	// The legitimate owner can still modify its symbols and commit.
	require.NoError(t, db.New(tx).DeleteAllOwnedSymbols(ctx, owner))
	require.NoError(t, tx.Commit(ctx))
	_, afterTarget := jsonReq(t, http.MethodGet, targetPath, "", standardHeaders)
	require.JSONEq(t, beforeTarget, afterTarget)
	requireDirectorySearch(t, `cql.serverChoice any "test:aninst stolen"`)
}
