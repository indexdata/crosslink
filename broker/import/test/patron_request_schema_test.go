package test

import (
	"os"
	"testing"

	"github.com/google/uuid"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatronRequestSchemaCompatibility(t *testing.T) {
	up, err := os.ReadFile("../../migrations/082_remove_patron_request_bib_record_id.up.sql")
	require.NoError(t, err)
	down, err := os.ReadFile("../../migrations/082_remove_patron_request_bib_record_id.down.sql")
	require.NoError(t, err)
	// This suite has no background app or parallel tests. Restore its shared
	// schema if a failure interrupts the migration round trip.
	hasLegacyColumn := false
	t.Cleanup(func() {
		if hasLegacyColumn {
			_, err := importTestPool.Exec(importTestCtx, string(up))
			require.NoError(t, err)
		}
	})
	prRepo := pr_db.CreatePrRepo(importTestPool, false)
	prefix := uuid.NewString()
	seed := testPatronBundle(prefix, prefix+"-request")
	seed.PatronRequest.IllRequest.BibliographicInfo.SupplierUniqueRecordId = "preserved-record"
	require.NoError(t, importOnly(seed))

	for _, schema := range []struct {
		name      string
		migration []byte
		legacy    bool
	}{
		{name: "upgraded"},
		{name: "rolled back", migration: down, legacy: true},
		{name: "reapplied", migration: up},
	} {
		if len(schema.migration) > 0 {
			_, err := importTestPool.Exec(importTestCtx, string(schema.migration))
			require.NoError(t, err)
			hasLegacyColumn = schema.legacy
		}
		t.Run(schema.name, func(t *testing.T) {
			for _, table := range []string{"patron_request", "patron_request_search_view"} {
				var exists bool
				require.NoError(t, importTestPool.QueryRow(importTestCtx, `
					SELECT EXISTS (SELECT 1 FROM information_schema.columns
					WHERE table_schema = current_schema() AND table_name = $1
					AND column_name = 'bib_record_id')`, table).Scan(&exists))
				assert.Equal(t, schema.legacy, exists, table)
			}
			assertPatronRequestReadable(t, prRepo, seed.PatronRequest.ID, seed.PatronRequest.IllRequest)

			prefix := uuid.NewString()
			bundle := testPatronBundle(prefix, prefix+"-request")
			bundle.PatronRequest.IllRequest.BibliographicInfo.SupplierUniqueRecordId = "source-record"
			created, err := prRepo.CreatePatronRequest(importTestCtx, bundle.PatronRequest)
			require.NoError(t, err)
			assertPatronRequestReadable(t, prRepo, created.ID, created.IllRequest)

			created.IllRequest.BibliographicInfo.SupplierUniqueRecordId = "updated-record"
			updated, err := prRepo.UpdatePatronRequest(importTestCtx, pr_db.UpdatePatronRequestParams(created))
			require.NoError(t, err)
			assertPatronRequestReadable(t, prRepo, updated.ID, created.IllRequest)

			prefix = uuid.NewString()
			bundle = testPatronBundle(prefix, prefix+"-request")
			for i, recordID := range []string{"imported-record", "reimported-record", ""} {
				bundle.PatronRequest.IllRequest.BibliographicInfo.SupplierUniqueRecordId = recordID
				policy := importdb.ConflictPolicyUpdate
				if i == 0 {
					policy = importdb.ConflictPolicyFail
				}
				_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, policy)
				require.NoError(t, err)
				assertPatronRequestReadable(t, prRepo, bundle.PatronRequest.ID, bundle.PatronRequest.IllRequest)
			}
		})
	}
}

func assertPatronRequestReadable(t *testing.T, prRepo pr_db.PrRepo, id string, expected iso18626.Request) {
	t.Helper()
	stored, err := prRepo.GetPatronRequestById(importTestCtx, id)
	require.NoError(t, err)
	assert.Equal(t, expected, stored.IllRequest)
	assert.Equal(t, expected.BibliographicInfo.SupplierUniqueRecordId, prservice.BibRecordID(stored.Side, stored.IllRequest))
	view, err := prRepo.GetPatronRequestSearchView(importTestCtx, id)
	require.NoError(t, err)
	assert.Equal(t, expected, view.IllRequest)

	query, err := pr_db.ParsePatronRequestsCql(`id = "` + id + `"`)
	require.NoError(t, err)
	params := pr_db.ListPatronRequestsParams{Limit: 10}
	views, count, err := prRepo.ListPatronRequestsSearchView(importTestCtx, params, query)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Len(t, views, 1)
	assert.Equal(t, expected, views[0].IllRequest)
	requests, count, err := prRepo.ListPatronRequests(importTestCtx, params, query)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Len(t, requests, 1)
	assert.Equal(t, expected, requests[0].IllRequest)
}
