package test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestExportItemsAndPickingState(t *testing.T) {
	source, err := os.ReadFile("../../../migration/export-crosslink-open-patron-requests.sql")
	require.NoError(t, err)

	// Run the production views against a minimal ReShare schema.
	const mapStart = "CREATE TEMP TABLE crosslink_state_map"
	_, setup, found := strings.Cut(string(source), mapStart)
	require.True(t, found, "state mapping setup must exist")
	setup, _, found = strings.Cut(setup, "-- A rota entry's lastStatus")
	require.True(t, found, "state mapping setup end must exist")
	const viewStart = "CREATE TEMP VIEW crosslink_request_ids"
	_, views, found := strings.Cut(string(source), viewStart)
	require.True(t, found, "request export views must exist")
	views, _, found = strings.Cut(views, "DO $skipped$")
	require.True(t, found, "request export views end must exist")
	_, itemsProjection, found := strings.Cut(string(source), "'items', ")
	require.True(t, found, "item export projection must exist")
	itemsProjection, _, found = strings.Cut(itemsProjection, ",\n            'notifications',")
	require.True(t, found, "item export projection end must exist")
	query := "SELECT import_state, " + itemsProjection + `
		FROM crosslink_open_requests AS request WHERE import_patron_request_id = $1`

	ctx := context.Background()
	tx, err := importTestPool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, tx.Rollback(ctx)) })
	_, err = tx.Exec(ctx, `
		CREATE TEMP TABLE patron_request (
			pr_id text PRIMARY KEY, pr_is_requester boolean DEFAULT false, pr_hrid text,
			pr_state_fk text, pr_state_model_fk text DEFAULT 'responder', pr_service_type_fk text,
			pr_selected_item_barcode text, pr_needs_attention boolean,
			pr_succeeded_by_fk text, pr_preceded_by_fk text, pr_peer_request_identifier text,
			pr_sup_inst_symbol text, pr_req_inst_symbol text, pr_rota_position integer,
			pr_service_level_fk text, pr_pub_type_fk text, pr_copyright_type_fk text,
			pr_maximum_costs_code_fk text, pr_cost_currency_fk text,
			pr_resolved_sup_inst_symbol_fk text, pr_resolved_pickup_location_fk text,
			pr_patron_email text, pr_local_call_number text, pr_title text,
			pr_external_hold_request_id text, pr_date_created timestamp DEFAULT '2026-01-01'
		);
		CREATE TEMP TABLE request_volume (
			rv_id text PRIMARY KEY, rv_patron_request_fk text, rv_status_fk text,
			rv_item_id text, rv_temporary_item_barcode text, rv_call_number text,
			rv_name text, rv_date_created timestamp
		);
		CREATE TEMP TABLE status (st_id text PRIMARY KEY, st_code text, st_needs_attention boolean);
		CREATE TEMP TABLE state_model (sm_id text PRIMARY KEY, sm_shortcode text);
		CREATE TEMP TABLE state_model_status (sms_state_model text, sms_state text, sms_is_terminal boolean);
		CREATE TEMP TABLE refdata_value (rdv_id text PRIMARY KEY, rdv_value text);
		CREATE TEMP TABLE crosslink_rota_suppliers (
			prr_patron_request_fk text, prr_rota_position integer, supplier_symbol text
		);
		CREATE TEMP TABLE symbol (sym_id text, sym_authority_fk text, sym_symbol text, sym_owner_fk text);
		CREATE TEMP TABLE naming_authority (na_id text, na_symbol text);
		CREATE TEMP TABLE directory_entry (de_id text, de_parent text);
		CREATE TEMP TABLE directory_entry_tag (directory_entry_tags_id text, tag_id text);
		CREATE TEMP TABLE tag (id text, norm_value text);

		INSERT INTO status (st_id, st_code) VALUES
			('picking', 'RES_AWAIT_PICKING'), ('pull-slip', 'RES_NEW_AWAIT_PULL_SLIP'),
			('error', 'RES_ERROR'), ('received', 'REQ_BORROWING_LIBRARY_RECEIVED');
		INSERT INTO state_model VALUES ('responder', 'Responder');
		INSERT INTO state_model_status SELECT 'responder', st_id, false FROM status;
		INSERT INTO refdata_value VALUES
			('loan', 'Loan'), ('copy-or-loan', 'Copy or Loan'), ('copy', 'Copy'),
			('requested', 'requested_from_the_ils'), ('cancelled', 'ils_request_cancelled');
	`)
	require.NoError(t, err)

	type exportedItem struct {
		ID           string `json:"id"`
		Barcode      string `json:"barcode"`
		LMSRequestID string `json:"lmsRequestId"`
		ItemID       string `json:"itemId"`
	}
	type itemExport struct {
		State string
		Items []exportedItem
	}
	cases := []struct {
		name, state, service string
		barcode              *string
		wantState            string
		wantItems            []exportedItem
	}{
		{"selected barcode", "picking", "loan", new("123456"), "SEARCHING",
			[]exportedItem{{ID: "selected barcode:selected-item", Barcode: "123456", LMSRequestID: "hold"}}},
		{"multiple volumes", "picking", "loan", new("first"), "SEARCHING",
			[]exportedItem{{ID: "volume-1", Barcode: "first", LMSRequestID: "hold"}, {ID: "volume-2", Barcode: "second"}}},
		{"volume without selected barcode", "picking", "loan", nil, "SEARCHING",
			[]exportedItem{{ID: "volume-3", Barcode: "third", LMSRequestID: "hold"}}},
		{"cancelled selected volume", "picking", "loan", new("cancelled"), "ITEM_PENDING", nil},
		{"no usable barcode", "picking", "loan", new("  "), "ITEM_PENDING", nil},
		{"copy or loan", "picking", "copy-or-loan", nil, "SEARCHING", nil},
		{"copy", "picking", "copy", nil, "WILL_SUPPLY", nil},
		{"awaiting pull slip", "pull-slip", "loan", nil, "WILL_SUPPLY", nil},
		{"errored loan with a volume", "error", "loan", nil, "ITEM_PENDING",
			[]exportedItem{{ID: "volume-7", Barcode: "seventh"}}},
		{"borrowed volume", "received", "loan", nil, "RECEIVED",
			[]exportedItem{{ID: "volume-8", Barcode: "temporary", LMSRequestID: "hold", ItemID: "supplier-barcode"}}},
	}
	for _, tc := range cases {
		_, err := tx.Exec(ctx, `INSERT INTO patron_request
			(pr_id, pr_state_fk, pr_service_type_fk, pr_selected_item_barcode, pr_external_hold_request_id)
			VALUES ($1, $2, $3, $4, 'hold')`, tc.name, tc.state, tc.service, tc.barcode)
		require.NoError(t, err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE patron_request SET pr_is_requester = true WHERE pr_id = 'borrowed volume';
		INSERT INTO request_volume
		(rv_id, rv_patron_request_fk, rv_item_id, rv_status_fk) VALUES
		('volume-1', 'multiple volumes', 'first', NULL),
		('volume-2', 'multiple volumes', ' second ', NULL),
		('volume-3', 'volume without selected barcode', 'third', 'requested'),
		('volume-4', 'cancelled selected volume', 'cancelled', 'cancelled'),
		('volume-5', 'no usable barcode', '  ', NULL),
		('volume-6', 'selected barcode', '  ', NULL),
		('volume-7', 'errored loan with a volume', 'seventh', NULL),
		('volume-8', 'borrowed volume', 'supplier-barcode', NULL);
		UPDATE request_volume SET rv_temporary_item_barcode = 'temporary' WHERE rv_id = 'volume-8';
	`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, mapStart+setup)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, viewStart+strings.ReplaceAll(views, ":'broker'", "'ISIL:BROKER'"))
	require.NoError(t, err)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := tx.Query(ctx, query, tc.name)
			require.NoError(t, err)
			got, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[itemExport])
			require.NoError(t, err, "expected exactly one export row for the request")
			require.Equal(t, tc.wantState, got.State)
			require.ElementsMatch(t, tc.wantItems, got.Items)
		})
	}
}
