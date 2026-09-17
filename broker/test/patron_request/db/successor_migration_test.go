package db

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/indexdata/crosslink/broker/app"
	"github.com/indexdata/crosslink/iso18626"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillSuccessorRequestType(t *testing.T) {
	migration, err := os.ReadFile("../../../migrations/065_backfill_successor_request_type.up.sql")
	require.NoError(t, err)
	conn, err := pgx.Connect(appCtx, app.ConnectionString)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.Close(appCtx))
	}()
	tx, err := conn.Begin(appCtx)
	require.NoError(t, err)
	defer func() {
		assert.NoError(t, tx.Rollback(appCtx))
	}()
	// Shadow the real table to exercise the migration without changing app data.
	_, err = tx.Exec(appCtx, `CREATE TEMP TABLE patron_request
		(LIKE public.patron_request INCLUDING DEFAULTS) ON COMMIT DROP`)
	require.NoError(t, err)

	for _, tc := range []struct {
		name        string
		serviceInfo string
	}{
		{"new", `,"serviceInfo":{"serviceType":"Copy","requestType":"New","requestingAgencyPreviousRequestId":"older","note":"keep"}`},
		{"missing type", `,"serviceInfo":{"serviceType":"Loan","note":"keep"}`},
		{"null type", `,"serviceInfo":{"serviceType":"Loan","requestType":null}`},
		{"missing service info", ``},
		{"null service info", `,"serviceInfo":null`},
		{"retry", `,"serviceInfo":{"serviceType":"Loan","requestType":"Retry"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tx.Exec(appCtx, `TRUNCATE patron_request`)
			require.NoError(t, err)
			original := `{"header":{"requestingAgencyRequestId":"successor"},"bibliographicInfo":{"title":"Keep title"}` + tc.serviceInfo + `}`
			_, err = tx.Exec(appCtx, `INSERT INTO patron_request (id, state, side, prev_req_id, ill_request)
				VALUES ('successor', 'CUSTOM_STATE', 'borrowing', 'deleted-predecessor', $1),
				       ('unlinked', 'CUSTOM_STATE', 'borrowing', NULL, $1),
				       ('lending', 'CUSTOM_STATE', 'lending', 'deleted-predecessor', $1)`, original)
			require.NoError(t, err)
			_, err = tx.Exec(appCtx, string(migration))
			require.NoError(t, err)
			var data []byte
			require.NoError(t, tx.QueryRow(appCtx, `SELECT ill_request FROM patron_request WHERE id = 'successor'`).Scan(&data))
			var expected map[string]any
			require.NoError(t, json.Unmarshal([]byte(original), &expected))
			serviceInfo, ok := expected["serviceInfo"].(map[string]any)
			if !ok {
				serviceInfo = map[string]any{"serviceType": "Loan"}
			}
			serviceInfo["requestType"] = "Retry"
			serviceInfo["requestingAgencyPreviousRequestId"] = "deleted-predecessor"
			expected["serviceInfo"] = serviceInfo
			expectedJSON, err := json.Marshal(expected)
			require.NoError(t, err)
			assert.JSONEq(t, string(expectedJSON), string(data))
			var request iso18626.Request
			require.NoError(t, json.Unmarshal(data, &request))
			require.NotNil(t, request.ServiceInfo)
			require.NotNil(t, request.ServiceInfo.RequestType)
			assert.Equal(t, iso18626.TypeRequestTypeRetry, *request.ServiceInfo.RequestType)
			assert.Equal(t, "deleted-predecessor", request.ServiceInfo.RequestingAgencyPreviousRequestId)
			assert.Equal(t, "successor", request.Header.RequestingAgencyRequestId)
			assert.Equal(t, "Keep title", request.BibliographicInfo.Title)
			if tc.name == "new" {
				assert.Equal(t, iso18626.TypeServiceTypeCopy, request.ServiceInfo.ServiceType)
			} else {
				assert.Equal(t, iso18626.TypeServiceTypeLoan, request.ServiceInfo.ServiceType)
			}
			for _, id := range []string{"unlinked", "lending"} {
				require.NoError(t, tx.QueryRow(appCtx, `SELECT ill_request FROM patron_request WHERE id = $1`, id).Scan(&data))
				assert.JSONEq(t, original, string(data))
			}
		})
	}
}
