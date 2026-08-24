package importdb

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/common"
	"github.com/indexdata/crosslink/broker/dbutil"
	ill_db "github.com/indexdata/crosslink/broker/ill_db"
	pr_db "github.com/indexdata/crosslink/broker/patron_request/db"
	test "github.com/indexdata/crosslink/broker/test/utils"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	importTestPool *pgxpool.Pool
	importTestRepo ImportRepo
	importTestCtx  = common.CreateExtCtxWithArgs(context.Background(), nil)
)

func TestMain(m *testing.M) {
	ctx, container, connectionString, err := test.StartPGContainer()
	if err != nil {
		panic(err)
	}
	connectionString += dbutil.SearchPath("crosslink_broker")
	if err = dbutil.RunDbProvision(connectionString, "crosslink_broker"); err != nil {
		panic(err)
	}
	if importTestPool, err = dbutil.InitDbPool(connectionString); err != nil {
		panic(err)
	}
	for _, schemaFile := range []string{"../../sqlc/ill_schema.sql", "../../sqlc/pr_schema.sql", "../../sqlc/event_schema.sql", "../../sqlc/sched_schema.sql"} {
		schema, readErr := os.ReadFile(schemaFile)
		if readErr != nil {
			panic(readErr)
		}
		if _, execErr := importTestPool.Exec(context.Background(), string(schema)); execErr != nil {
			panic(execErr)
		}
	}
	importTestRepo = CreateImportRepo(importTestPool)
	code := m.Run()
	importTestPool.Close()
	if err := test.TerminatePGContainer(ctx, container); err != nil {
		panic(err)
	}
	os.Exit(code)
}

func TestImportPatronRequestInsertsAndSynchronizesCompleteBundle(t *testing.T) {
	prefix := uuid.NewString()
	requestID := prefix + "-request"
	bundle := testPatronBundle(prefix, requestID)
	inserted, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, ConflictPolicyFail)
	require.NoError(t, err)
	assert.Equal(t, OutcomeImported, inserted.Outcome)

	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM item WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM notification WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM ill_transaction WHERE id=$1", bundle.IllTransaction.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM located_supplier WHERE ill_transaction_id=$1", bundle.IllTransaction.ID))
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM event WHERE patron_request_id=$1", bundle.PatronRequest.ID))

	failResult, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, ConflictPolicyFail)
	assert.Empty(t, failResult.Outcome)
	var conflict *ConflictError
	assert.ErrorAs(t, err, &conflict)

	skipped, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, ConflictPolicySkip)
	require.NoError(t, err)
	assert.Equal(t, OutcomeSkipped, skipped.Outcome)

	bundle.PatronRequest.Patron = pgtype.Text{String: "updated-patron", Valid: true}
	bundle.Items = []pr_db.SaveItemParams{{ID: prefix + "-item-new", Barcode: "new", CreatedAt: testTimestamp(4)}}
	bundle.Notifications = nil
	bundle.LocatedSuppliers = nil
	updated, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, ConflictPolicyUpdate)
	require.NoError(t, err)
	assert.Equal(t, OutcomeImported, updated.Outcome)
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM item WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM notification WHERE pr_id=$1", bundle.PatronRequest.ID))
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM located_supplier WHERE ill_transaction_id=$1", bundle.IllTransaction.ID))
	var patron string
	require.NoError(t, importTestPool.QueryRow(context.Background(), "SELECT patron FROM patron_request WHERE id=$1", bundle.PatronRequest.ID).Scan(&patron))
	assert.Equal(t, "updated-patron", patron)
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
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, second, ConflictPolicyFail)
	require.Error(t, err)
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", second.PatronRequest.ID))
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM item WHERE id=$1 AND pr_id=$2", first.Items[0].ID, first.PatronRequest.ID))
}

func TestImportPatronRequestCannotOmitExistingIllAssociation(t *testing.T) {
	prefix := uuid.NewString()
	bundle := testPatronBundle(prefix, prefix+"-request")
	require.NoError(t, importOnly(bundle))
	bundle.IllTransaction = nil
	bundle.LocatedSuppliers = nil
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, ConflictPolicyUpdate)
	require.ErrorContains(t, err, "cannot be omitted")
	assert.Equal(t, 1, queryCount(t, "SELECT count(*) FROM ill_transaction WHERE requester_request_id=$1", bundle.PatronRequest.RequesterReqID.String))
}

func TestImportPatronRequestNewRootCannotReuseExistingIllTransaction(t *testing.T) {
	prefix := uuid.NewString()
	original := testPatronBundle(prefix+"-original", prefix+"-request")
	require.NoError(t, importOnly(original))
	incoming := testPatronBundle(prefix+"-incoming", prefix+"-request")
	incoming.IllTransaction.ID = original.IllTransaction.ID
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, incoming, ConflictPolicyUpdate)
	require.ErrorContains(t, err, "already belongs to a persisted aggregate")
	assert.Equal(t, 0, queryCount(t, "SELECT count(*) FROM patron_request WHERE id=$1", incoming.PatronRequest.ID))
}

func testPatronBundle(prefix, requesterRequestID string) PatronRequestBundle {
	requesterPeerID := prefix + "-requester-peer"
	supplierPeerID := prefix + "-supplier-peer"
	insertPeer(prefix+"-REQ", requesterPeerID)
	insertPeer(prefix+"-SUP", supplierPeerID)
	return PatronRequestBundle{
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
func importOnly(bundle PatronRequestBundle) error {
	_, err := importTestRepo.ImportPatronRequest(importTestCtx, bundle, ConflictPolicyFail)
	return err
}
func queryCount(t *testing.T, query string, args ...any) int {
	t.Helper()
	var count int
	require.NoError(t, importTestPool.QueryRow(context.Background(), query, args...).Scan(&count))
	return count
}
