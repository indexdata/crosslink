package test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	ill_db "github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	"github.com/indexdata/crosslink/broker/import/service"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/indexdata/crosslink/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	importTestPool *pgxpool.Pool
	importTestRepo importdb.ImportRepo
	importTestCtx  = common.CreateExtCtxWithArgs(context.Background(), nil)
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	app.DB_PROVISION = true
	pgContainer, err := testutil.RunPostgres(ctx)
	test.Expect(err, "failed to start db container")

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	test.Expect(err, "failed to get conn string")

	app.ConnectionString = connStr
	app.MigrationsFolder = "file://../../migrations"
	test.Expect(app.RunDbUp(), "failed to run db migrations")

	importTestPool, err = app.InitDbPool()
	test.Expect(err, "failed to init db pool")

	importTestRepo = importdb.CreateImportRepo(importTestPool)
	code := m.Run()
	importTestPool.Close()
	if err := test.TerminatePGContainer(ctx, pgContainer); err != nil {
		panic(err)
	}
	os.Exit(code)
}

func TestImportPatronRequestInsertsAndSynchronizesCompleteBundle(t *testing.T) {
	prefix := uuid.NewString()
	requestID := prefix + "-request"
	bundle := testPatronBundle(prefix, requestID)
	inserted, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyFail)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, inserted.Outcome)

	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM item WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM notification WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM ill_transaction WHERE id=$1", bundle.IllTransaction.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM located_supplier WHERE ill_transaction_id=$1", bundle.IllTransaction.ID))
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM event WHERE patron_request_id=$1", bundle.PatronRequest.ID))

	failResult, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyFail)
	assert.Empty(t, failResult.Outcome)
	var conflict *importdb.ConflictError
	assert.ErrorAs(t, err, &conflict)

	skipped, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicySkip)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeSkipped, skipped.Outcome)

	bundle.PatronRequest.Patron = pgtype.Text{String: "updated-patron", Valid: true}
	bundle.Items = []pr_db.SaveItemParams{{ID: prefix + "-item-new", Barcode: "new", CreatedAt: testTimestamp(4)}}
	bundle.Notifications = nil
	bundle.LocatedSuppliers = nil
	updated, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, updated.Outcome)
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM item WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM notification WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM located_supplier WHERE ill_transaction_id=$1", bundle.IllTransaction.ID))
	var patron string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT patron FROM patron_request WHERE id=$1", bundle.PatronRequest.ID).Scan(&patron))
	assert.Equal(t, "updated-patron", patron)
}

func TestImportPatronRequestUpdateLeavesLiveBorrowingRequests(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyFail)
	require.NoError(t, err)
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM imported_patron_request WHERE id=$1", bundle.PatronRequest.ID))
	// The request now looks like one created by the broker, as a live request
	// that took the ID before the import reserved it would.
	_, err = importTestPool.Exec(context.Background(), "DELETE FROM imported_patron_request WHERE id=$1", bundle.PatronRequest.ID)
	require.NoError(t, err)

	bundle.PatronRequest.Patron = pgtype.Text{String: "legacy-patron", Valid: true}
	_, err = importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyUpdate)
	var conflict *importdb.ConflictError
	require.ErrorAs(t, err, &conflict)
	assert.Contains(t, conflict.Reason, "not created by an import")
	var patron pgtype.Text
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT patron FROM patron_request WHERE id=$1", bundle.PatronRequest.ID).Scan(&patron))
	assert.NotEqual(t, "legacy-patron", patron.String, "the live request keeps its data")

	skipped, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicySkip)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeSkipped, skipped.Outcome)
}

func TestImportPatronRequestUpdatePreservesStoredItemLocations(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	bundle.Items[0].Location = pgtype.Text{String: "Main Library", Valid: true}
	bundle.Items[0].ShelvingLocation = pgtype.Text{String: "Stacks", Valid: true}
	require.NoError(t, importOnly(bundle))

	bundle.Items[0].Location = pgtype.Text{}
	bundle.Items[0].ShelvingLocation = pgtype.Text{}
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyUpdate)
	require.NoError(t, err)

	var location, shelvingLocation pgtype.Text
	require.NoError(t, importTestPool.QueryRow(
		context.Background(),
		"SELECT location, shelving_location FROM item WHERE id=$1",
		bundle.Items[0].ID,
	).Scan(&location, &shelvingLocation))
	assert.Equal(t, pgtype.Text{String: "Main Library", Valid: true}, location)
	assert.Equal(t, pgtype.Text{String: "Stacks", Valid: true}, shelvingLocation)
}

func TestImportPatronRequestPersistsPickupLocation(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	first, second := uuid.New(), uuid.New()
	bundle.PatronRequest.RequesterPickupLocationID = pgtype.UUID{Bytes: first, Valid: true}
	require.NoError(t, importOnly(bundle))
	assert.Equal(t, pgtype.UUID{Bytes: first, Valid: true}, pickupLocationValue(t, bundle.PatronRequest.ID))

	bundle.PatronRequest.RequesterPickupLocationID = pgtype.UUID{Bytes: second, Valid: true}
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, pgtype.UUID{Bytes: second, Valid: true}, pickupLocationValue(t, bundle.PatronRequest.ID))
}

func TestImportPatronRequestPersistsDueDate(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	for _, tc := range []struct {
		name   string
		due    pgtype.Timestamptz
		policy importdb.ConflictPolicy
	}{
		{name: "insert", due: pgtype.Timestamptz{Time: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC), Valid: true}, policy: importdb.ConflictPolicyFail},
		{name: "update", due: pgtype.Timestamptz{Time: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC), Valid: true}, policy: importdb.ConflictPolicyUpdate},
		{name: "clear", policy: importdb.ConflictPolicyUpdate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bundle.PatronRequest.DueAt = tc.due
			_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, tc.policy)
			require.NoError(t, err)
			var due pgtype.Timestamptz
			require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT due_at FROM patron_request WHERE id=$1", bundle.PatronRequest.ID).Scan(&due))
			assert.Equal(t, tc.due.Valid, due.Valid)
			if tc.due.Valid {
				assert.True(t, tc.due.Time.Equal(due.Time))
			}
		})
	}
}

func TestImportPatronRequestDoesNotWriteTriggerManagedItemsCache(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	bundle.Items = nil
	bundle.Notifications = nil
	bundle.IllTransaction = nil
	bundle.LocatedSuppliers = nil
	bundle.PatronRequest.Items = []pr_db.PrItem{{ID: "cache-from-import"}}

	require.NoError(t, importOnly(bundle))
	assert.Equal(t, 0, queryCount(t, "SELECT jsonb_array_length(items) FROM patron_request WHERE id=$1", bundle.PatronRequest.ID))

	_, err := importTestPool.Exec(context.Background(), `UPDATE patron_request SET items = '[{"id":"trigger-owned"}]'::jsonb WHERE id=$1`, bundle.PatronRequest.ID)
	require.NoError(t, err)
	bundle.PatronRequest.Items = []pr_db.PrItem{{ID: "cache-from-update"}}
	updated, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, updated.Outcome)

	var cachedItemID string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT items->0->>'id' FROM patron_request WHERE id=$1", bundle.PatronRequest.ID).Scan(&cachedItemID))
	assert.Equal(t, "trigger-owned", cachedItemID)
}

func TestImportPatronRequestUpdateRejectsIdentityChanges(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*importdb.PatronRequestBundle)
		mutate  func(*importdb.PatronRequestBundle)
	}{
		{
			name:    "side",
			prepare: func(*importdb.PatronRequestBundle) {},
			mutate: func(bundle *importdb.PatronRequestBundle) {
				bundle.PatronRequest.Side = "lending"
			},
		},
		{
			name:    "borrowing owner symbol",
			prepare: func(*importdb.PatronRequestBundle) {},
			mutate: func(bundle *importdb.PatronRequestBundle) {
				bundle.PatronRequest.RequesterSymbol = pgtype.Text{String: "ISIL:OTHER-REQUESTER", Valid: true}
			},
		},
		{
			name: "lending owner symbol",
			prepare: func(bundle *importdb.PatronRequestBundle) {
				bundle.PatronRequest.Side = "lending"
				bundle.PatronRequest.SupplierSymbol = pgtype.Text{String: "ISIL:SUPPLIER", Valid: true}
			},
			mutate: func(bundle *importdb.PatronRequestBundle) {
				bundle.PatronRequest.SupplierSymbol = pgtype.Text{String: "ISIL:OTHER-SUPPLIER", Valid: true}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prefix := uuid.NewString()
			bundle := testPatronBundle(prefix, prefix+"-request")
			bundle.IllTransaction = nil
			bundle.LocatedSuppliers = nil
			tt.prepare(&bundle)
			expectedSide := bundle.PatronRequest.Side
			expectedRequesterSymbol := bundle.PatronRequest.RequesterSymbol
			expectedSupplierSymbol := bundle.PatronRequest.SupplierSymbol
			require.NoError(t, importOnly(bundle))

			tt.mutate(&bundle)
			_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyUpdate)

			var conflict *importdb.ConflictError
			require.ErrorAs(t, err, &conflict)
			var side string
			var requesterSymbol, supplierSymbol pgtype.Text
			require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT side, requester_symbol, supplier_symbol FROM patron_request WHERE id=$1", bundle.PatronRequest.ID).Scan(&side, &requesterSymbol, &supplierSymbol))
			assert.Equal(t, string(expectedSide), side)
			assert.Equal(t, expectedRequesterSymbol, requesterSymbol)
			assert.Equal(t, expectedSupplierSymbol, supplierSymbol)
		})
	}
}

func TestImportLendingPatronRequestAppliesPolicyToRoutingIdentity(t *testing.T) {
	tests := []struct {
		policy          importdb.ConflictPolicy
		expectedOutcome importdb.Outcome
		expectedPatron  string
		expectConflict  bool
	}{
		{policy: importdb.ConflictPolicyFail, expectedPatron: "original", expectConflict: true},
		{policy: importdb.ConflictPolicySkip, expectedOutcome: importdb.OutcomeSkipped, expectedPatron: "original"},
		{policy: importdb.ConflictPolicyUpdate, expectedOutcome: importdb.OutcomeImported, expectedPatron: "updated"},
	}

	for _, tt := range tests {
		t.Run(string(tt.policy), func(t *testing.T) {
			prefix := uuid.NewString()
			existing := testLendingPatronBundle(prefix+"-existing", prefix+"-request", prefix+"-SUP")
			require.NoError(t, importOnly(existing))

			incoming := testLendingPatronBundle(prefix+"-incoming", prefix+"-request", prefix+"-SUP")
			incoming.PatronRequest.Patron = pgtype.Text{String: "updated", Valid: true}
			result, err := importTestRepo.ImportPatronRequest(importTestCtx, incoming, tt.policy)
			if tt.expectConflict {
				var conflict *importdb.ConflictError
				require.ErrorAs(t, err, &conflict)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedOutcome, result.Outcome)
			}

			assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", incoming.PatronRequest.ID))
			var patron string
			require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT patron FROM patron_request WHERE id=$1", existing.PatronRequest.ID).Scan(&patron))
			assert.Equal(t, tt.expectedPatron, patron)
		})
	}
}

func TestImportLendingPatronRequestRejectsDifferentIDAndRoutingIdentityMatches(t *testing.T) {
	for _, policy := range []importdb.ConflictPolicy{
		importdb.ConflictPolicyFail,
		importdb.ConflictPolicySkip,
		importdb.ConflictPolicyUpdate,
	} {
		t.Run(string(policy), func(t *testing.T) {
			prefix := uuid.NewString()
			byID := testLendingPatronBundle(prefix+"-id", prefix+"-request-id", prefix+"-SUP-ID")
			byRoutingIdentity := testLendingPatronBundle(prefix+"-route", prefix+"-request-route", prefix+"-SUP-ROUTE")
			require.NoError(t, importOnly(byID))
			require.NoError(t, importOnly(byRoutingIdentity))

			incoming := byID
			incoming.PatronRequest.RequesterReqID = byRoutingIdentity.PatronRequest.RequesterReqID
			incoming.PatronRequest.SupplierSymbol = byRoutingIdentity.PatronRequest.SupplierSymbol
			result, err := importTestRepo.ImportPatronRequest(importTestCtx, incoming, policy)

			var conflict *importdb.ConflictError
			require.ErrorAs(t, err, &conflict)
			assert.Empty(t, result.Outcome)
			assert.Equal(t, "original", patronValue(t, byID.PatronRequest.ID))
			assert.Equal(t, "original", patronValue(t, byRoutingIdentity.PatronRequest.ID))
		})
	}
}

func TestImportLendingPatronRequestAllowsRequesterRequestIDAssociatedWithBorrowingAggregate(t *testing.T) {
	prefix := uuid.NewString()
	requesterRequestID := prefix + "-request"
	borrowing := testPatronBundle(prefix+"-borrowing", requesterRequestID)
	require.NoError(t, importOnly(borrowing))

	lending := testLendingPatronBundle(prefix+"-lending", requesterRequestID, prefix+"-lending-SUP")
	result, err := importTestRepo.ImportPatronRequest(importTestCtx, lending, importdb.ConflictPolicyFail)

	require.NoError(t, err)
	assert.Equal(t, importdb.OutcomeImported, result.Outcome)
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", borrowing.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", lending.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM ill_transaction WHERE requester_request_id=$1", requesterRequestID))
}

func TestImportPatronRequestCollisionRollsBackRoot(t *testing.T) {
	prefix := uuid.NewString()
	first := testPatronBundle(prefix+"-first", prefix+"-request-first")
	first.IllTransaction = nil
	first.LocatedSuppliers = nil
	require.NoError(t, importOnly(first))

	second := testPatronBundle(prefix+"-second", prefix+"-request-second")
	second.IllTransaction = nil
	second.LocatedSuppliers = nil
	second.Items[0].ID = first.Items[0].ID
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, second, importdb.ConflictPolicyFail)
	require.Error(t, err)
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", second.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM item WHERE id=$1 AND pr_id=$2", first.Items[0].ID, first.PatronRequest.ID))
}

func TestSaveItemDoesNotReparentAfterConcurrentMissingChecks(t *testing.T) {
	prefix := uuid.NewString()
	first := testPatronBundle(prefix+"-first", prefix+"-request-first")
	second := testPatronBundle(prefix+"-second", prefix+"-request-second")
	for _, bundle := range []*importdb.PatronRequestBundle{&first, &second} {
		bundle.Items = nil
		bundle.Notifications = nil
		bundle.IllTransaction = nil
		bundle.LocatedSuppliers = nil
		require.NoError(t, importOnly(*bundle))
	}

	childID := prefix + "-shared-item"
	tx1, err := importTestPool.Begin(context.Background())
	require.NoError(t, err)
	defer func() {
		_ = tx1.Rollback(context.Background())
	}()
	tx2, err := importTestPool.Begin(context.Background())
	require.NoError(t, err)
	defer func() {
		_ = tx2.Rollback(context.Background())
	}()
	assertMissingChild(t, tx1, "item", childID)
	assertMissingChild(t, tx2, "item", childID)

	queries := pr_db.New()
	_, err = queries.SaveItem(context.Background(), tx1, pr_db.SaveItemParams{ID: childID, PrID: first.PatronRequest.ID, Barcode: "first", CreatedAt: testTimestamp(2)})
	require.NoError(t, err)
	require.NoError(t, tx1.Commit(context.Background()))
	_, err = queries.SaveItem(context.Background(), tx2, pr_db.SaveItemParams{ID: childID, PrID: second.PatronRequest.ID, Barcode: "second", CreatedAt: testTimestamp(3)})
	if err == nil {
		require.NoError(t, tx2.Commit(context.Background()))
	} else {
		require.NoError(t, tx2.Rollback(context.Background()))
	}

	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM item WHERE id=$1 AND pr_id=$2", childID, first.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM patron_request, jsonb_array_elements(items) AS child WHERE patron_request.id=$1 AND child->>'id'=$2", first.PatronRequest.ID, childID))
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM patron_request, jsonb_array_elements(items) AS child WHERE patron_request.id=$1 AND child->>'id'=$2", second.PatronRequest.ID, childID))
}

func TestSaveNotificationDoesNotReparentAfterConcurrentMissingChecks(t *testing.T) {
	prefix := uuid.NewString()
	first := testPatronBundle(prefix+"-first", prefix+"-request-first")
	second := testPatronBundle(prefix+"-second", prefix+"-request-second")
	for _, bundle := range []*importdb.PatronRequestBundle{&first, &second} {
		bundle.Items = nil
		bundle.Notifications = nil
		bundle.IllTransaction = nil
		bundle.LocatedSuppliers = nil
		require.NoError(t, importOnly(*bundle))
	}

	childID := prefix + "-shared-notification"
	tx1, err := importTestPool.Begin(context.Background())
	require.NoError(t, err)
	defer func() {
		_ = tx1.Rollback(context.Background())
	}()
	tx2, err := importTestPool.Begin(context.Background())
	require.NoError(t, err)
	defer func() {
		_ = tx2.Rollback(context.Background())
	}()
	assertMissingChild(t, tx1, "notification", childID)
	assertMissingChild(t, tx2, "notification", childID)

	queries := pr_db.New()
	firstNotification := pr_db.SaveNotificationParams{ID: childID, PrID: first.PatronRequest.ID, FromSymbol: prefix + "-REQ", ToSymbol: prefix + "-SUP", Direction: pr_db.NotificationDirectionSent, Kind: pr_db.NotificationKindNote, CreatedAt: testTimestamp(2)}
	_, err = queries.SaveNotification(context.Background(), tx1, firstNotification)
	require.NoError(t, err)
	require.NoError(t, tx1.Commit(context.Background()))
	firstNotification.PrID = second.PatronRequest.ID
	_, err = queries.SaveNotification(context.Background(), tx2, firstNotification)
	if err == nil {
		require.NoError(t, tx2.Commit(context.Background()))
	} else {
		require.NoError(t, tx2.Rollback(context.Background()))
	}

	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM notification WHERE id=$1 AND pr_id=$2", childID, first.PatronRequest.ID))
}

func TestSaveLocatedSupplierDoesNotReparentAfterConcurrentMissingChecks(t *testing.T) {
	prefix := uuid.NewString()
	firstPrefix := prefix + "-first"
	secondPrefix := prefix + "-second"
	first := testPatronBundle(firstPrefix, prefix+"-request-first")
	second := testPatronBundle(secondPrefix, prefix+"-request-second")
	for _, bundle := range []*importdb.PatronRequestBundle{&first, &second} {
		bundle.Items = nil
		bundle.Notifications = nil
		bundle.LocatedSuppliers = nil
		require.NoError(t, importOnly(*bundle))
	}

	childID := prefix + "-shared-located-supplier"
	tx1, err := importTestPool.Begin(context.Background())
	require.NoError(t, err)
	defer func() {
		_ = tx1.Rollback(context.Background())
	}()
	tx2, err := importTestPool.Begin(context.Background())
	require.NoError(t, err)
	defer func() {
		_ = tx2.Rollback(context.Background())
	}()
	assertMissingChild(t, tx1, "located_supplier", childID)
	assertMissingChild(t, tx2, "located_supplier", childID)

	queries := ill_db.New()
	firstSupplier := ill_db.SaveLocatedSupplierParams{ID: childID, IllTransactionID: first.IllTransaction.ID, SupplierID: firstPrefix + "-supplier-peer", SupplierSymbol: firstPrefix + "-SUP", Ordinal: 1}
	_, err = queries.SaveLocatedSupplier(context.Background(), tx1, firstSupplier)
	require.NoError(t, err)
	require.NoError(t, tx1.Commit(context.Background()))
	firstSupplier.IllTransactionID = second.IllTransaction.ID
	_, err = queries.SaveLocatedSupplier(context.Background(), tx2, firstSupplier)
	if err == nil {
		require.NoError(t, tx2.Commit(context.Background()))
	} else {
		require.NoError(t, tx2.Rollback(context.Background()))
	}

	assert.ErrorIs(t, err, pgx.ErrNoRows)
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM located_supplier WHERE id=$1 AND ill_transaction_id=$2", childID, first.IllTransaction.ID))
}

func TestImportPatronRequestCannotOmitExistingIllAssociation(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	require.NoError(t, importOnly(bundle))
	bundle.IllTransaction = nil
	bundle.LocatedSuppliers = nil
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyUpdate)
	require.ErrorContains(t, err, "cannot be omitted")
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM ill_transaction WHERE requester_request_id=$1", bundle.PatronRequest.RequesterReqID.String))
}

func TestImportPatronRequestNewRootCannotReuseExistingIllTransaction(t *testing.T) {
	prefix := uuid.NewString()
	original := testPatronBundle(prefix+"-original", prefix+"-request")
	require.NoError(t, importOnly(original))
	incoming := testPatronBundle(prefix+"-incoming", prefix+"-request")
	incoming.IllTransaction.ID = original.IllTransaction.ID
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, incoming, importdb.ConflictPolicyUpdate)
	require.ErrorContains(t, err, "already belongs to a persisted aggregate")
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", incoming.PatronRequest.ID))
}

func testPatronBundle(prefix, requesterRequestID string) importdb.PatronRequestBundle {
	requesterPeerID := prefix + "-requester-peer"
	supplierPeerID := prefix + "-supplier-peer"
	insertPeer(prefix+"-REQ", requesterPeerID)
	insertPeer(prefix+"-SUP", supplierPeerID)
	return importdb.PatronRequestBundle{
		PatronRequest:    pr_db.CreatePatronRequestParams{ID: prefix + "-pr", CreatedAt: testTimestamp(0), UpdatedAt: testTimestamp(1), IllRequest: iso18626.Request{}, State: "SENT", Side: "borrowing", Patron: pgtype.Text{String: "original", Valid: true}, RequesterSymbol: pgtype.Text{String: prefix + "-REQ", Valid: true}, Tenant: pgtype.Text{String: "tenant", Valid: true}, RequesterReqID: pgtype.Text{String: requesterRequestID, Valid: true}, Items: []pr_db.PrItem{}, Language: pr_db.LANGUAGE, StateModel: "default"},
		Items:            []pr_db.SaveItemParams{{ID: prefix + "-item", Barcode: "barcode", CreatedAt: testTimestamp(2)}},
		Notifications:    []pr_db.SaveNotificationParams{{ID: prefix + "-notification", FromSymbol: prefix + "-REQ", ToSymbol: prefix + "-SUP", Direction: pr_db.NotificationDirectionSent, Kind: pr_db.NotificationKindNote, CreatedAt: testTimestamp(3)}},
		IllTransaction:   &ill_db.SaveIllTransactionParams{ID: prefix + "-ill", Timestamp: testTimestamp(0), RequesterSymbol: pgtype.Text{String: prefix + "-REQ", Valid: true}, RequesterID: pgtype.Text{String: requesterPeerID, Valid: true}, RequesterRequestID: pgtype.Text{String: requesterRequestID, Valid: true}, IllTransactionData: ill_db.IllTransactionData{}},
		LocatedSuppliers: []ill_db.SaveLocatedSupplierParams{{ID: prefix + "-located", SupplierID: supplierPeerID, SupplierSymbol: prefix + "-SUP", Ordinal: 1}},
	}
}

func testLendingPatronBundle(prefix, requesterRequestID, supplierSymbol string) importdb.PatronRequestBundle {
	bundle := testPatronBundle(prefix, requesterRequestID)
	bundle.PatronRequest.Side = "lending"
	bundle.PatronRequest.SupplierSymbol = pgtype.Text{String: supplierSymbol, Valid: true}
	bundle.Items = nil
	bundle.Notifications = nil
	bundle.IllTransaction = nil
	bundle.LocatedSuppliers = nil
	return bundle
}

func patronValue(t *testing.T, id string) string {
	t.Helper()
	var patron string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT patron FROM patron_request WHERE id=$1", id).Scan(&patron))
	return patron
}

func pickupLocationValue(t *testing.T, id string) pgtype.UUID {
	t.Helper()
	var pickupLocation pgtype.UUID
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT requester_pickup_location_id FROM patron_request WHERE id=$1", id).Scan(&pickupLocation))
	return pickupLocation
}

func insertPeer(symbol, id string) {
	_, err := importTestPool.Exec(context.Background(), `INSERT INTO peer (id,name,refresh_policy,url,vendor,broker_mode) VALUES ($1,$1,'never','http://example.test','test','transparent') ON CONFLICT DO NOTHING`, id)
	if err != nil {
		panic(err)
	}
	_, err = importTestPool.Exec(context.Background(), `INSERT INTO symbol (symbol_value,peer_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, symbol, id)
	if err != nil {
		panic(err)
	}
}

func testTimestamp(offset int) pgtype.Timestamp {
	return pgtype.Timestamp{Time: time.Date(2026, 8, 1, 10, offset, 0, 0, time.UTC), Valid: true}
}
func importOnly(bundle importdb.PatronRequestBundle) error {
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyFail)
	return err
}
func queryCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	require.NoError(t, importTestPool.QueryRow(context.Background(), query, args...).Scan(&count))
	return count
}

func assertMissingChild(t *testing.T, tx pgx.Tx, table, id string) {
	t.Helper()
	var foundID string
	err := tx.QueryRow(context.Background(), "SELECT id FROM "+table+" WHERE id=$1", id).Scan(&foundID)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestImportPatronRequestPersistsBibRecordID(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	for i, expected := range []pgtype.Text{{String: "shared-record", Valid: true}, {String: "updated-record", Valid: true}, {}} {
		bundle.PatronRequest.BibRecordID = expected
		policy := importdb.ConflictPolicyUpdate
		if i == 0 {
			policy = importdb.ConflictPolicyFail
		}
		_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, policy)
		require.NoError(t, err)
		var actual pgtype.Text
		require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT bib_record_id FROM patron_request WHERE id=$1", bundle.PatronRequest.ID).Scan(&actual))
		assert.Equal(t, expected, actual)
	}
}

func TestImportPatronRequestLeavesHridSequenceToReservation(t *testing.T) {
	// The importer reserves HRIDs for the whole stream before importing records,
	// so a record import takes no sequence lock and leaves the sequence alone.
	hridPrefix := "T" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	bundle.PatronRequest.ID = hridPrefix + "-41"
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, importdb.ConflictPolicyFail)
	require.NoError(t, err)
	var hrid string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT get_next_hrid($1)", hridPrefix).Scan(&hrid))
	assert.Equal(t, strings.ToUpper(hridPrefix)+"-1", hrid)
}

func TestReserveHridsWaitsForConcurrentSequenceUse(t *testing.T) {
	// Counts waiters on this prefix's sequence lock; a bigint advisory key is
	// stored as classid (high 32 bits) and objid (low 32 bits).
	const waitingQuery = `WITH lock_key AS (
		SELECT hashtextextended('broker:hrid-seq:' || lower($1) || '_hrid_seq', 0) AS k
	)
	SELECT count(*) FROM pg_locks, lock_key
	WHERE locktype = 'advisory' AND NOT granted AND objsubid = 1
	  AND classid::bigint = (k >> 32) & 4294967295
	  AND objid::bigint = k & 4294967295`
	for _, tc := range []struct {
		name, blockerSQL, wantNext string
	}{
		{name: "uncommitted advance", blockerSQL: "SELECT advance_hrid($1, 90)", wantNext: "-91"},
		{name: "uncommitted request creation", blockerSQL: "SELECT get_next_hrid($1)", wantNext: "-42"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			hridPrefix := "T" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
			blocker, err := importTestPool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = blocker.Rollback(ctx) })
			_, err = blocker.Exec(ctx, tc.blockerSQL, hridPrefix)
			require.NoError(t, err)

			done := make(chan error, 1)
			go func() {
				done <- importTestRepo.ReserveHrids(importTestCtx, map[string]int64{strings.ToUpper(hridPrefix): 41})
			}()
			require.Eventually(t, func() bool {
				var waiting int
				err := importTestPool.QueryRow(ctx, waitingQuery, hridPrefix).Scan(&waiting)
				return err == nil && waiting > 0
			}, 5*time.Second, 10*time.Millisecond, "the reservation must wait for the sequence lock")
			select {
			case err := <-done:
				t.Fatalf("reservation finished while the sequence lock was held: %v", err)
			default:
			}

			require.NoError(t, blocker.Commit(ctx))
			require.NoError(t, <-done)
			var hrid string
			require.NoError(t, importTestPool.QueryRow(ctx, "SELECT get_next_hrid($1)", hridPrefix).Scan(&hrid))
			assert.Equal(t, strings.ToUpper(hridPrefix)+tc.wantNext, hrid)
		})
	}
}

func TestReserveHridsSupportsIsilPrefixes(t *testing.T) {
	base := "T" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
	reservations := map[string]int64{base + "-X": 41, base + "/X": 7, base + ":X": 1000, strings.ToUpper(base) + "-Y": 3}
	require.NoError(t, importTestRepo.ReserveHrids(importTestCtx, reservations))
	// Reserving a lower number later never moves a sequence back.
	require.NoError(t, importTestRepo.ReserveHrids(importTestCtx, map[string]int64{base + "-X": 5}))
	for prefix, used := range reservations {
		var hrid string
		require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT get_next_hrid($1)", prefix).Scan(&hrid))
		assert.Equal(t, strings.ToUpper(prefix)+"-"+strconv.FormatInt(used+1, 10), hrid)
	}
}

// stubPrefixResolver resolves request ID prefixes from a map, failing for
// symbols it does not know.
type stubPrefixResolver map[string]string

func (r stubPrefixResolver) LookupRequestIdPrefix(_ common.ExtendedContext, symbol string) (string, error) {
	prefix, ok := r[symbol]
	if !ok {
		return "", errors.New("unknown requester symbol")
	}
	return prefix, nil
}

// liveCreationRepo creates a request ID the way request creation does after
// the first patron request record is imported.
type liveCreationRepo struct {
	importdb.ImportRepo
	prefix  string
	created []string
}

func (r *liveCreationRepo) ImportPatronRequest(ctx common.ExtendedContext, bundle importdb.PatronRequestBundle, policy importdb.ConflictPolicy) (importdb.Result, error) {
	result, err := r.ImportRepo.ImportPatronRequest(ctx, bundle, policy)
	if err == nil && len(r.created) == 0 {
		var hrid string
		err = importTestPool.QueryRow(context.Background(), "SELECT get_next_hrid($1)", r.prefix).Scan(&hrid)
		r.created = append(r.created, hrid)
	}
	return result, err
}

func borrowingImportRecord(t *testing.T, symbolValue, id string) string {
	t.Helper()
	record := map[string]any{
		"type": "patronRequest", "owner": "ISIL:" + symbolValue,
		"data": map[string]any{
			"patronRequest": map[string]any{
				"id": id, "createdAt": testTimestamp(0).Time, "updatedAt": testTimestamp(1).Time,
				"state": "SENT", "side": "borrowing", "stateModel": "default", "needsAttention": false,
				"requesterSymbol": "ISIL:" + symbolValue, "requesterRequestId": id,
				"illRequest": map[string]any{
					"header": map[string]any{
						"requestingAgencyId": map[string]any{"agencyIdType": map[string]string{"#text": "ISIL"}, "agencyIdValue": symbolValue},
						"supplyingAgencyId":  map[string]any{"agencyIdType": map[string]string{"#text": "ISIL"}, "agencyIdValue": symbolValue + "-SUP"},
						"timestamp":          testTimestamp(0).Time, "requestingAgencyRequestId": id,
					},
					"bibliographicInfo": map[string]string{"title": "Reservation"},
					"serviceInfo":       map[string]string{"serviceType": "Loan"},
				},
			},
			"items": []any{}, "notifications": []any{}, "locatedSuppliers": []any{},
		},
	}
	line, err := json.Marshal(record)
	require.NoError(t, err)
	return string(line) + "\n"
}

func TestImportReservesHridsOfLaterRecords(t *testing.T) {
	// A request created after the first record is imported must not take the
	// ID of a later record in the same stream.
	symbolValue := "T" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	hridPrefix := "P/" + symbolValue[:8]
	insertPeer("ISIL:"+symbolValue, symbolValue+"-peer")
	repo := &liveCreationRepo{ImportRepo: importTestRepo, prefix: hridPrefix}
	importer := service.NewImporter(repo, ill_db.CreateIllRepo(importTestPool), nil, &prservice.StateModelService{},
		stubPrefixResolver{"ISIL:" + symbolValue: hridPrefix}, time.Now)
	body := borrowingImportRecord(t, symbolValue, hridPrefix+"-41") + borrowingImportRecord(t, symbolValue, hridPrefix+"-42")

	result, err := importer.Import(importTestCtx, importdb.ConflictPolicyFail, strings.NewReader(body))

	require.NoError(t, err)
	require.Empty(t, result.Errors)
	assert.EqualValues(t, 2, result.PatronRequests.Imported)
	assert.Equal(t, []string{strings.ToUpper(hridPrefix) + "-43"}, repo.created)
}
