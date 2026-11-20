package test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestExportAwaitPickingStateMapping(t *testing.T) {
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
			pr_patron_email text
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
			('picking', 'RES_AWAIT_PICKING'), ('pull-slip', 'RES_NEW_AWAIT_PULL_SLIP'), ('error', 'RES_ERROR');
		INSERT INTO state_model VALUES ('responder', 'Responder');
		INSERT INTO state_model_status SELECT 'responder', st_id, false FROM status;
		INSERT INTO refdata_value VALUES ('loan', 'Loan'), ('copy-or-loan', 'Copy or Loan'), ('copy', 'Copy');
	`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, mapStart+setup)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, viewStart+strings.ReplaceAll(views, ":'broker'", "'ISIL:BROKER'"))
	require.NoError(t, err)

	for _, tc := range []struct {
		name, state, service string
		barcode              *string
		want                 string
	}{
		{"loan with barcode", "picking", "loan", new("123456"), "SEARCHING"},
		{"loan without barcode", "picking", "loan", nil, "ITEM_PENDING"},
		{"loan with blank barcode", "picking", "loan", new("  "), "ITEM_PENDING"},
		{"copy or loan", "picking", "copy-or-loan", nil, "SEARCHING"},
		{"copy", "picking", "copy", nil, "WILL_SUPPLY"},
		{"awaiting pull slip", "pull-slip", "loan", nil, "WILL_SUPPLY"},
		{"errored loan", "error", "loan", new("123456"), "ITEM_PENDING"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tx.Exec(ctx, `INSERT INTO patron_request
				(pr_id, pr_state_fk, pr_service_type_fk, pr_selected_item_barcode)
				VALUES ($1, $2, $3, $4)`, tc.name, tc.state, tc.service, tc.barcode)
			require.NoError(t, err)
			rows, err := tx.Query(ctx, `SELECT import_state FROM crosslink_open_requests
				WHERE import_patron_request_id = $1`, tc.name)
			require.NoError(t, err)
			state, err := pgx.CollectExactlyOneRow(rows, pgx.RowTo[string])
			require.NoError(t, err, "expected exactly one export row for the request")
			require.Equal(t, tc.want, state)
		})
	}
}
