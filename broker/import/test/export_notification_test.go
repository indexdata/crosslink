package test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/indexdata/crosslink/broker/ill_db"
	importdb "github.com/indexdata/crosslink/broker/import/db"
	"github.com/indexdata/crosslink/broker/import/service"
	prservice "github.com/indexdata/crosslink/broker/patron_request/service"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"
)

func TestExportLoanConditionNotificationsImportCompatibility(t *testing.T) {
	// Execute the actual notification projection, including its JSON field names,
	// without requiring the full legacy schema or interpreting psql commands.
	source, err := os.ReadFile("../../../migration/export-crosslink-open-patron-requests.sql")
	require.NoError(t, err)
	_, projection, found := strings.Cut(string(source), "'notifications', ")
	require.True(t, found, "notification projection must exist")
	projection, _, found = strings.Cut(projection, ",\n            'illTransaction',")
	require.True(t, found, "notification projection end must exist")
	query := `SELECT ` + projection + ` FROM (
		SELECT $1::text AS pr_id, $2::text AS import_side,
		       $3::text AS pr_req_inst_symbol, $4::text AS import_supplier_symbol
	) AS request`

	for _, tc := range []struct {
		name      string
		side      string
		code      pgtype.Text
		direction string
	}{
		{"borrowing coded condition", "borrowing", pgtype.Text{String: "NoReproduction", Valid: true}, "received"},
		{"lending coded condition", "lending", pgtype.Text{String: "LibraryUseOnly", Valid: true}, "sent"},
		{"cost-only condition", "borrowing", pgtype.Text{}, "received"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			prefix := uuid.NewString()
			requestID, notificationID := prefix+"-request", prefix+"-condition"
			requester, supplier := "ISIL:"+prefix+"-REQ", "ISIL:"+prefix+"-SUP"
			insertPeer(requester, prefix+"-requester-peer")
			insertPeer(supplier, prefix+"-supplier-peer")

			tx, err := importTestPool.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
			_, err = tx.Exec(ctx, `
				CREATE TEMP TABLE patron_request_notification (
					prn_id text, prn_patron_request_fk text, prn_timestamp timestamp,
					prn_date_created timestamp, prn_is_sender boolean, prn_sender_symbol text,
					prn_message_content text, prn_action_data text, prn_action_status text,
					prn_attached_action text, prn_message_status text, prn_seen boolean,
					prn_last_updated timestamp
				) ON COMMIT DROP;
				CREATE TEMP TABLE patron_request_loan_condition (
					prlc_id text, prlc_patron_request_fk text, prlc_date_created timestamp,
					prlc_sup_inst_symbol text, prlc_note text, prlc_cost numeric,
					prlc_cost_currency_fk text, prlc_code text
				) ON COMMIT DROP;
				CREATE TEMP TABLE refdata_value (rdv_id text, rdv_value text) ON COMMIT DROP;
				INSERT INTO refdata_value VALUES ('currency', 'USD');
			`)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, `INSERT INTO patron_request_loan_condition
				VALUES ($1, $2, $3, $4, 'Handle with care', 12.50, 'currency', $5)`,
				notificationID, requestID, testTimestamp(3), supplier, tc.code)
			require.NoError(t, err)
			var raw json.RawMessage
			require.NoError(t, tx.QueryRow(ctx, query, requestID, tc.side, requester, supplier).Scan(&raw))

			var notifications []map[string]any
			require.NoError(t, json.Unmarshal(raw, &notifications))
			require.Len(t, notifications, 1)
			notification := notifications[0]
			require.Equal(t, "condition", notification["kind"])
			require.NotContains(t, notification, "condition")
			if tc.code.Valid {
				require.Equal(t, tc.code.String, notification["code"])
			} else {
				require.NotContains(t, notification, "code")
			}
			require.Equal(t, tc.direction, notification["direction"])
			require.Equal(t, supplier, notification["fromSymbol"])
			require.Equal(t, requester, notification["toSymbol"])
			require.Equal(t, "Handle with care", notification["note"])
			require.Equal(t, 12.5, notification["cost"])
			require.Equal(t, "USD", notification["currency"])

			owner, state := requester, "SENT"
			if tc.side == "lending" {
				owner, state = supplier, "VALIDATED"
			}
			record := map[string]any{
				"type": "patronRequest", "owner": owner,
				"data": map[string]any{
					"patronRequest": map[string]any{
						"id": requestID, "createdAt": testTimestamp(0).Time, "updatedAt": testTimestamp(1).Time,
						"state": state, "side": tc.side, "stateModel": "default", "needsAttention": false,
						"requesterSymbol": requester, "supplierSymbol": supplier, "requesterRequestId": requestID,
						"illRequest": map[string]any{
							"header": map[string]any{
								"requestingAgencyId": map[string]any{"agencyIdType": map[string]string{"#text": "ISIL"}, "agencyIdValue": prefix + "-REQ"},
								"supplyingAgencyId":  map[string]any{"agencyIdType": map[string]string{"#text": "ISIL"}, "agencyIdValue": prefix + "-SUP"},
								"timestamp":          testTimestamp(0).Time, "requestingAgencyRequestId": requestID,
							},
							"bibliographicInfo": map[string]string{"title": "Export compatibility"},
							"serviceInfo":       map[string]string{"serviceType": "Loan"},
						},
					},
					"notifications": raw, "items": []any{}, "locatedSuppliers": []any{},
				},
			}
			var ndjson bytes.Buffer
			require.NoError(t, json.NewEncoder(&ndjson).Encode(record))
			importer := service.NewImporter(importTestRepo, ill_db.CreateIllRepo(importTestPool), nil, &prservice.StateModelService{}, time.Now)
			result, err := importer.Import(importTestCtx, importdb.ConflictPolicyFail, &ndjson)
			require.NoError(t, err)
			require.Empty(t, result.Errors)
			require.EqualValues(t, 1, result.PatronRequests.Imported)
			require.Zero(t, result.PatronRequests.Failed)

			var kind, direction, fromSymbol, toSymbol, note, currency string
			var code pgtype.Text
			var cost float64
			require.NoError(t, importTestPool.QueryRow(ctx, `
				SELECT kind, code, direction, from_symbol, to_symbol, note, cost::float8, currency
				FROM notification WHERE id=$1 AND pr_id=$2`, notificationID, requestID,
			).Scan(&kind, &code, &direction, &fromSymbol, &toSymbol, &note, &cost, &currency))
			require.Equal(t, "condition", kind)
			require.Equal(t, tc.code, code)
			require.Equal(t, tc.direction, direction)
			require.Equal(t, supplier, fromSymbol)
			require.Equal(t, requester, toSymbol)
			require.Equal(t, "Handle with care", note)
			require.Equal(t, 12.5, cost)
			require.Equal(t, "USD", currency)
		})
	}
}
