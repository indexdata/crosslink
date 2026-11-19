package test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEntryConfigJSONMigration(t *testing.T) {
	ctx := context.Background()
	tx, err := dbpool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	exec := func(query string, args ...any) {
		t.Helper()
		_, err := tx.Exec(ctx, query, args...)
		require.NoError(t, err)
	}
	// Build a real version 027 schema without changing the integration suite's tables.
	exec("CREATE SCHEMA entry_config_json_migration_test; SET LOCAL search_path TO entry_config_json_migration_test")
	apply := func(version int, direction string) {
		t.Helper()
		paths, err := filepath.Glob(fmt.Sprintf("../migrations/%03d_*.%s.sql", version, direction))
		require.NoError(t, err)
		require.Len(t, paths, 1)
		data, err := os.ReadFile(paths[0])
		require.NoError(t, err)
		exec(string(data))
	}
	for version := 1; version <= 27; version++ {
		apply(version, "up")
	}

	full, sparse := uuid.New(), uuid.New()
	exec(`INSERT INTO entries (id, name, type) VALUES ($1, 'Full', 'Institution'), ($2, 'Sparse', 'Institution')`, full, sparse)
	exec(`INSERT INTO ill_configs (entry, iso18626_url, iso18626_vendor, lenders_of_last_resort,
		include_requesting_agency_info, include_supplier_info, include_return_info, include_vendor_note, use_offered_costs,
		note_field_separator, supplier_patron_pattern, duplicate_check_window_hours, default_loan_period, max_requests_per_patron,
		is_pickup_location, minimum_cost, load_balancing_policy,
		unfilled_reasons, cancellation_reasons, loan_conditions, available_copyright_types, default_copyright_type, request_id_prefix)
		VALUES ($1, 'https://peer.example/iso18626', 'Alma', '{ISIL:DK-1,ALT:ALIAS}',
		true, false, true, false, true,
		' | ', '{requesterSymbol}', 24, 28, 5,
		true, 1.25, 'deficit',
		'[{"code":"U1","description":"Unfilled"}]', '[{"code":"C1","description":"Cancelled"}]',
		'[{"code":"L1","description":"Library use only"}]', '[{"code":"US"}]', '{"code":"US"}', 'AB-1')`, full)
	exec(`INSERT INTO lms_configs (entry, address, from_agency, from_agency_authentication, to_agency,
		lookup_user_enabled, accept_item_enabled, checkin_item_enabled, checkout_item_enabled, item_location,
		request_item_request_type, request_item_scope_type, request_item_bib_code, request_item_pickup_location_enabled,
		requester_pickup_location, supplier_pickup_location, requester_patron_pattern, request_item_enabled, patron_profiles,
		vendor, ncip_namespace_enabled, bib_id_normalization, ncip_version, bib_item_scope_name)
		VALUES ($1, 'https://ncip.example', 'FROM', 'secret', 'TO',
		true, false, true, false, 'Main',
		'Hold', 'Item', 'SYSNUMBER', true,
		'Desk', 'Shipping', '{patronId}', false, '[{"name":"Staff","canCreateRequests":true}]',
		'Alma', true, 'strip', '2.02', 'Bib')`, full)
	exec(`INSERT INTO catalog_configs (entry, metadata_update_mode, sru_address, sru_record_schema, zoom_address,
		query_type, query_identifier, query_isbn, query_issn, query_title,
		holdings_marc_call_number_subfield, holdings_marc_item_id_subfield, holdings_marc_location_subfield,
		holdings_marc_main_field, holdings_marc_restricted_subfield, holdings_marc_shelving_location_subfield,
		holdings_marc21plus1_enabled, holdings_opac_enabled, holdings_reservoir_enabled,
		metadata_marc21_author, metadata_marc21_edition, metadata_marc21_identifier, metadata_marc21_isbn,
		metadata_marc21_issn, metadata_marc21_subtitle, metadata_marc21_title,
		zoom_options, profile, holdings_config, query_year, query_identifier_normalization, query_identifier_prefix,
		metadata_marc21_publisher, metadata_marc21_publication_date, metadata_marc21_place_of_publication)
		VALUES ($1, 'replace', 'https://sru.example', 'marcxml', 'z3950.example:210',
		'cql', 'rec.id = {term}', 'isbn = {term}', 'issn = {term}', 'title = {term}',
		'h', 'p', 'b',
		'852', 'r', 'c',
		false, true, false,
		'100$a', '250$a', '001', '020$a',
		'022$a', '245$b', '245$a',
		'{"preferredRecordSyntax":"usmarc"}', 'folio',
		'{"marc":{"mainField":"852","callNumberSubfield":"h","itemIdSubfield":"p","locationSubfield":"b","restrictedSubfield":"r","shelvingLocationSubfield":"c"},"opac":{}}',
		'year = {term}', 'strip', 'ocm',
		'264*1$b', '264*1$c', '264*1$a')`, full)
	exec(`INSERT INTO holdings_policies (entry, policy) VALUES ($1, '{"locations":[{"code":"MAIN","name":"Main"}]}')`, full)
	// An empty lender list blocks inheritance, and LMS address and agency are
	// required columns that an absent value restores as empty.
	exec(`INSERT INTO ill_configs (entry, lenders_of_last_resort) VALUES ($1, '{}')`, sparse)
	exec(`INSERT INTO lms_configs (entry, address, from_agency) VALUES ($1, '', '')`, sparse)

	tables := []string{"ill_configs", "lms_configs", "catalog_configs", "holdings_policies"}
	snapshot := func() map[string]string {
		t.Helper()
		rows := map[string]string{}
		for _, table := range tables {
			var data string
			require.NoError(t, tx.QueryRow(ctx, fmt.Sprintf(
				`SELECT COALESCE(jsonb_agg(to_jsonb(t) - 'id' ORDER BY t.entry), '[]')::text FROM %s t`, table)).Scan(&data))
			rows[table] = data
		}
		return rows
	}
	before := snapshot()

	apply(28, "up")
	configs := func(id uuid.UUID) [4]*string {
		t.Helper()
		var c [4]*string
		require.NoError(t, tx.QueryRow(ctx,
			`SELECT ill_config::text, lms_config::text, catalog_config::text, holdings_policy::text FROM entries WHERE id=$1`, id).
			Scan(&c[0], &c[1], &c[2], &c[3]))
		return c
	}
	got := configs(full)
	for i, want := range []string{
		`{"iso18626Url":"https://peer.example/iso18626","iso18626Vendor":"Alma",
			"lendersOfLastResort":[{"authority":"ISIL","symbol":"DK-1"},{"authority":"ALT","symbol":"ALIAS"}],
			"includeRequestingAgencyInfo":true,"includeSupplierInfo":false,"includeReturnInfo":true,"includeVendorNote":false,
			"useOfferedCosts":true,"noteFieldSeparator":" | ","supplierPatronPattern":"{requesterSymbol}",
			"duplicateCheckWindowHours":24,"defaultLoanPeriod":28,"maxRequestsPerPatron":5,"isPickupLocation":true,
			"minimumCost":1.25,"loadBalancingPolicy":"deficit",
			"unfilledReasons":[{"code":"U1","description":"Unfilled"}],"cancellationReasons":[{"code":"C1","description":"Cancelled"}],
			"loanConditions":[{"code":"L1","description":"Library use only"}],"availableCopyrightTypes":[{"code":"US"}],
			"defaultCopyrightType":{"code":"US"},"requestIdPrefix":"AB-1"}`,
		`{"address":"https://ncip.example","fromAgency":"FROM","fromAgencyAuthentication":"secret","toAgency":"TO",
			"lookupUserEnabled":true,"acceptItemEnabled":false,"checkInItemEnabled":true,"checkOutItemEnabled":false,
			"itemLocation":"Main","requestItemRequestType":"Hold","requestItemRequestScopeType":"Item",
			"requestItemBibIdCode":"SYSNUMBER","requestItemPickupLocationEnabled":true,"requesterPickupLocation":"Desk",
			"supplierPickupLocation":"Shipping","requesterPatronPattern":"{patronId}","requestItemEnabled":false,
			"patronProfiles":[{"name":"Staff","canCreateRequests":true}],"vendor":"Alma","ncipNamespaceEnabled":true,
			"bibIdNormalization":"strip","ncipVersion":"2.02","bibItemScopeName":"Bib"}`,
		`{"profile":"folio","metadataUpdateMode":"replace",
			"sru":{"address":"https://sru.example","recordSchema":"marcxml"},
			"zoom":{"address":"z3950.example:210","options":{"preferredRecordSyntax":"usmarc"}},
			"queryConfig":{"type":"cql","identifier":"rec.id = {term}","isbn":"isbn = {term}","issn":"issn = {term}",
				"title":"title = {term}","year":"year = {term}","identifierNormalization":"strip","identifierPrefix":"ocm"},
			"holdingsFormat":{"marc":{"mainField":"852","callNumberSubfield":"h","itemIdSubfield":"p","locationSubfield":"b",
				"restrictedSubfield":"r","shelvingLocationSubfield":"c"},"opac":{}},
			"metadataFormat":{"marc21":{"author":"100$a","edition":"250$a","identifier":"001","isbn":"020$a","issn":"022$a",
				"subtitle":"245$b","title":"245$a","publisher":"264*1$b","publicationDate":"264*1$c","placeOfPublication":"264*1$a"}}}`,
		`{"locations":[{"code":"MAIN","name":"Main"}]}`,
	} {
		require.NotNil(t, got[i], tables[i])
		require.JSONEq(t, want, *got[i], tables[i])
	}
	got = configs(sparse)
	require.JSONEq(t, `{"lendersOfLastResort":[]}`, *got[0])
	require.JSONEq(t, `{}`, *got[1])
	require.Nil(t, got[2])
	require.Nil(t, got[3])

	apply(28, "down")
	after := snapshot()
	for _, table := range tables {
		require.JSONEq(t, before[table], after[table], table)
	}
}
