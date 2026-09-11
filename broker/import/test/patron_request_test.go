package test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/broker/common"
	ill_db "github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
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
