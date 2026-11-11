// Package storedconfig loads the configuration containers that directory
// entries store locally, as the JSON objects entry reads return.
package storedconfig

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Columns projects an entry's stored ill_config, lms_config, catalog_config
// and holdings_policy as JSON objects, in that order. It expects the entries
// table aliased as e.
const Columns = `
		(
		SELECT json_strip_nulls(json_build_object(
			'isPickupLocation', i.is_pickup_location,
			'iso18626Url', i.iso18626_url,
			'iso18626Vendor', i.iso18626_vendor,
			-- json_agg over no rows is NULL; keep a stored empty list as [] so it blocks inheritance.
			'lendersOfLastResort', CASE WHEN i.lenders_of_last_resort IS NULL THEN NULL ELSE COALESCE((
				SELECT json_agg(json_build_object(
					'authority', split_part(value, ':', 1),
					'symbol', substring(value from position(':' in value) + 1)
				))
				FROM unnest(i.lenders_of_last_resort) value
			), '[]'::json) END,
			'includeRequestingAgencyInfo', i.include_requesting_agency_info,
			'includeSupplierInfo', i.include_supplier_info,
			'includeReturnInfo', i.include_return_info,
			'includeVendorNote', i.include_vendor_note,
			'useOfferedCosts', i.use_offered_costs,
			'noteFieldSeparator', i.note_field_separator,
			'supplierPatronPattern', i.supplier_patron_pattern,
			'requestIdPrefix', i.request_id_prefix,
			'duplicateCheckWindowHours', i.duplicate_check_window_hours,
			'defaultLoanPeriod', i.default_loan_period,
			'maxRequestsPerPatron', i.max_requests_per_patron,
			'minimumCost', i.minimum_cost,
			'loadBalancingPolicy', i.load_balancing_policy,
			'defaultCopyrightType', i.default_copyright_type,
			'availableCopyrightTypes', i.available_copyright_types,
			'unfilledReasons', i.unfilled_reasons,
			'loanConditions', i.loan_conditions,
			'cancellationReasons', i.cancellation_reasons
		)) FROM ill_configs i WHERE i.entry = e.id) as ill_config,
		(
		SELECT 
			json_strip_nulls(json_build_object(
				'vendor', l.vendor,
				'ncipNamespaceEnabled', l.ncip_namespace_enabled,
				'ncipVersion', l.ncip_version,
				'bibIdNormalization', l.bib_id_normalization,
				'acceptItemEnabled', l.accept_item_enabled,
				'address', NULLIF(l.address, ''),
				'checkInItemEnabled', l.checkin_item_enabled,
				'checkOutItemEnabled', l.checkout_item_enabled,
				'fromAgency', NULLIF(l.from_agency, ''),
				'fromAgencyAuthentication', l.from_agency_authentication,
				'itemLocation', l.item_location,
				'lookupUserEnabled', l.lookup_user_enabled,
				'requestItemBibIdCode', l.request_item_bib_code,
				'requestItemEnabled', l.request_item_enabled,
				'requestItemPickupLocationEnabled', l.request_item_pickup_location_enabled,
				'requestItemRequestScopeType', l.request_item_scope_type,
				'bibItemScopeName', l.bib_item_scope_name,
				'requestItemRequestType', l.request_item_request_type,
				'requesterPatronPattern', l.requester_patron_pattern,
				'requesterPickupLocation', l.requester_pickup_location,
				'supplierPickupLocation', l.supplier_pickup_location,
				'patronProfiles', l.patron_profiles,
				'toAgency', l.to_agency
			))
		from lms_configs l WHERE l.entry = e.id) as lms_config,
		(
		SELECT
			json_strip_nulls(json_build_object(
				'profile', h.profile,
				'metadataUpdateMode', h.metadata_update_mode,
				'sru', CASE WHEN h.sru_address IS NULL THEN NULL ELSE json_strip_nulls(json_build_object(
					'address', h.sru_address,
					'recordSchema', h.sru_record_schema
				)) END,
				'zoom', CASE WHEN h.zoom_address IS NULL THEN NULL ELSE json_strip_nulls(json_build_object(
					'address', h.zoom_address,
					'options', h.zoom_options
				)) END,
				'queryConfig', CASE WHEN h.query_type IS NULL AND h.query_identifier IS NULL AND h.query_isbn IS NULL AND h.query_issn IS NULL AND h.query_title IS NULL AND h.query_year IS NULL AND h.query_identifier_normalization IS NULL AND h.query_identifier_prefix IS NULL THEN NULL ELSE json_strip_nulls(json_build_object(
					'type', h.query_type,
					'identifier', h.query_identifier,
					'isbn', h.query_isbn,
					'issn', h.query_issn,
					'title', h.query_title,
					'year', h.query_year,
					'identifierNormalization', h.query_identifier_normalization,
					'identifierPrefix', h.query_identifier_prefix
				)) END,
				'holdingsFormat', COALESCE(h.holdings_config, NULLIF(jsonb_strip_nulls(jsonb_build_object(
					'marc', CASE WHEN h.holdings_marc_call_number_subfield IS NULL
						AND h.holdings_marc_item_id_subfield IS NULL
						AND h.holdings_marc_location_subfield IS NULL
						AND h.holdings_marc_main_field IS NULL
						AND h.holdings_marc_restricted_subfield IS NULL
						AND h.holdings_marc_shelving_location_subfield IS NULL THEN NULL ELSE json_strip_nulls(json_build_object(
							'callNumberSubField', h.holdings_marc_call_number_subfield,
							'itemIdSubField', h.holdings_marc_item_id_subfield,
							'locationSubField', h.holdings_marc_location_subfield,
							'mainField', h.holdings_marc_main_field,
							'restrictedSubField', h.holdings_marc_restricted_subfield,
							'shelvingLocationSubField', h.holdings_marc_shelving_location_subfield
						)) END,
					'marc21plus1', CASE WHEN h.holdings_marc21plus1_enabled THEN json_build_object() ELSE NULL END,
					'opac', CASE WHEN h.holdings_opac_enabled THEN json_build_object() ELSE NULL END,
					'reservoir', CASE WHEN h.holdings_reservoir_enabled THEN json_build_object() ELSE NULL END
				)), '{}'::jsonb)),
				'metadataFormat', CASE WHEN h.metadata_marc21_author IS NULL
					AND h.metadata_marc21_edition IS NULL
					AND h.metadata_marc21_identifier IS NULL
					AND h.metadata_marc21_isbn IS NULL
					AND h.metadata_marc21_issn IS NULL
					AND h.metadata_marc21_subtitle IS NULL
					AND h.metadata_marc21_publisher IS NULL
					AND h.metadata_marc21_publication_date IS NULL
					AND h.metadata_marc21_place_of_publication IS NULL
					AND h.metadata_marc21_title IS NULL THEN NULL ELSE json_strip_nulls(json_build_object(
						'marc21', json_strip_nulls(json_build_object(
							'author', h.metadata_marc21_author,
							'edition', h.metadata_marc21_edition,
							'identifier', h.metadata_marc21_identifier,
							'isbn', h.metadata_marc21_isbn,
							'issn', h.metadata_marc21_issn,
							'subtitle', h.metadata_marc21_subtitle,
							'publisher', h.metadata_marc21_publisher,
							'publicationDate', h.metadata_marc21_publication_date,
							'placeOfPublication', h.metadata_marc21_place_of_publication,
							'title', h.metadata_marc21_title
						))
					)) END
				)
			)
		from catalog_configs h WHERE h.entry = e.id) as catalog_config,
		(SELECT jsonb_strip_nulls(hp.policy) FROM holdings_policies hp WHERE hp.entry = e.id) as holdings_policy`

// Configs maps a configuration container's API property name, such as
// lmsConfig, to the JSON object an entry stores locally.
type Configs map[string]json.RawMessage

// FromColumns builds Configs from the four columns of Columns, omitting
// containers the entry does not store.
func FromColumns(illConfig, lmsConfig, catalogConfig, holdingsPolicy []byte) Configs {
	configs := Configs{}
	for name, value := range map[string][]byte{
		"illConfig": illConfig, "lmsConfig": lmsConfig, "catalogConfig": catalogConfig, "holdingsPolicy": holdingsPolicy,
	} {
		if value != nil {
			configs[name] = value
		}
	}
	return configs
}

// Querier is satisfied by both a connection pool and a transaction.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Entry is an entry's place in the hierarchy and its stored configuration.
type Entry struct {
	Type    string
	Parent  *uuid.UUID
	Tenant  *string
	Configs Configs
}

const loadSQL = `SELECT e.id, e.type, e.parent, e.tenant,` + Columns + `
	FROM entries e WHERE e.id = ANY($1)`

// Load returns the stored configuration of the entries with the given IDs.
// Missing entries are absent from the result.
func Load(ctx context.Context, q Querier, ids []uuid.UUID) (map[uuid.UUID]Entry, error) {
	rows, err := q.Query(ctx, loadSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("query stored entry configuration: %w", err)
	}
	defer rows.Close()
	result := make(map[uuid.UUID]Entry, len(ids))
	for rows.Next() {
		var (
			id                                     uuid.UUID
			entry                                  Entry
			illConfig, lmsConfig, catalog, holding []byte
		)
		if err := rows.Scan(&id, &entry.Type, &entry.Parent, &entry.Tenant, &illConfig, &lmsConfig, &catalog, &holding); err != nil {
			return nil, fmt.Errorf("scan stored entry configuration: %w", err)
		}
		entry.Configs = FromColumns(illConfig, lmsConfig, catalog, holding)
		result[id] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate stored entry configuration: %w", err)
	}
	return result, nil
}

// LoadLocal returns one entry's stored configuration, or nil if the entry
// does not exist.
func LoadLocal(ctx context.Context, q Querier, id uuid.UUID) (Configs, error) {
	entries, err := Load(ctx, q, []uuid.UUID{id})
	if err != nil {
		return nil, err
	}
	return entries[id].Configs, nil
}
