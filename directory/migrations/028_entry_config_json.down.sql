-- Restore the version 027 configuration tables from entries.configs. Keep
-- columns in their original order for callers that scan SELECT * by position.
-- Values that violate a restored column type or constraint fail the migration.
CREATE TABLE ill_configs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  entry uuid NOT NULL UNIQUE REFERENCES entries (id) ON DELETE CASCADE,
  iso18626_url text,
  iso18626_vendor varchar(64),
  lenders_of_last_resort text[],
  include_requesting_agency_info boolean,
  include_supplier_info boolean,
  include_return_info boolean,
  include_vendor_note boolean,
  use_offered_costs boolean,
  note_field_separator text,
  supplier_patron_pattern text,
  duplicate_check_window_hours integer CHECK (duplicate_check_window_hours >= 0),
  default_loan_period integer CHECK (default_loan_period >= 0),
  max_requests_per_patron integer CHECK (max_requests_per_patron >= 0),
  is_pickup_location boolean,
  minimum_cost double precision CHECK (minimum_cost >= 0),
  load_balancing_policy text CHECK (load_balancing_policy IN ('deficit', 'proportional')),
  unfilled_reasons jsonb CHECK (jsonb_typeof(unfilled_reasons) = 'array'),
  cancellation_reasons jsonb CHECK (jsonb_typeof(cancellation_reasons) = 'array'),
  loan_conditions jsonb CHECK (jsonb_typeof(loan_conditions) = 'array'),
  available_copyright_types jsonb CHECK (jsonb_typeof(available_copyright_types) = 'array'),
  default_copyright_type jsonb CHECK (jsonb_typeof(default_copyright_type) = 'object'),
  request_id_prefix text CHECK (request_id_prefix ~ '^[A-Za-z0-9:-]{0,16}$')
);

CREATE TABLE lms_configs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  entry uuid NOT NULL UNIQUE REFERENCES entries (id) ON DELETE CASCADE,
  address text NOT NULL,
  from_agency varchar(128) NOT NULL,
  from_agency_authentication text,
  to_agency varchar(128),
  lookup_user_enabled boolean,
  accept_item_enabled boolean,
  checkin_item_enabled boolean,
  checkout_item_enabled boolean,
  item_location varchar(255),
  request_item_request_type varchar(32),
  request_item_scope_type varchar(32),
  request_item_bib_code varchar(128),
  request_item_pickup_location_enabled boolean,
  requester_pickup_location varchar(128),
  supplier_pickup_location varchar(128),
  requester_patron_pattern varchar(255),
  request_item_enabled boolean,
  patron_profiles jsonb,
  vendor text,
  ncip_namespace_enabled boolean,
  bib_id_normalization text,
  ncip_version text CHECK (ncip_version IN ('1.0', '2.02', 'default')),
  bib_item_scope_name text
);

CREATE TABLE catalog_configs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  entry uuid NOT NULL UNIQUE REFERENCES entries (id) ON DELETE CASCADE,
  metadata_update_mode varchar(32),
  sru_address text,
  sru_record_schema varchar(255),
  zoom_address text,
  query_type varchar(16),
  query_identifier text,
  query_isbn text,
  query_issn text,
  query_title text,
  holdings_marc_call_number_subfield varchar(32),
  holdings_marc_item_id_subfield varchar(32),
  holdings_marc_location_subfield varchar(32),
  holdings_marc_main_field varchar(32),
  holdings_marc_restricted_subfield varchar(32),
  holdings_marc_shelving_location_subfield varchar(32),
  holdings_marc21plus1_enabled boolean,
  holdings_opac_enabled boolean,
  holdings_reservoir_enabled boolean,
  metadata_marc21_author varchar(32),
  metadata_marc21_edition varchar(32),
  metadata_marc21_identifier varchar(32),
  metadata_marc21_isbn varchar(32),
  metadata_marc21_issn varchar(32),
  metadata_marc21_subtitle varchar(32),
  metadata_marc21_title varchar(32),
  zoom_options jsonb,
  profile text,
  holdings_config jsonb,
  query_year text,
  query_identifier_normalization text,
  query_identifier_prefix text,
  metadata_marc21_publisher text,
  metadata_marc21_publication_date text,
  metadata_marc21_place_of_publication text
);

CREATE TABLE holdings_policies (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  entry uuid NOT NULL UNIQUE REFERENCES entries (id) ON DELETE CASCADE,
  policy jsonb NOT NULL
);

INSERT INTO ill_configs (
  entry, iso18626_url, iso18626_vendor, lenders_of_last_resort,
  include_requesting_agency_info, include_supplier_info, include_return_info, include_vendor_note, use_offered_costs,
  note_field_separator, supplier_patron_pattern, duplicate_check_window_hours, default_loan_period, max_requests_per_patron,
  is_pickup_location, minimum_cost, load_balancing_policy,
  unfilled_reasons, cancellation_reasons, loan_conditions, available_copyright_types, default_copyright_type, request_id_prefix
)
SELECT e.id, c->>'iso18626Url', c->>'iso18626Vendor',
  -- An empty list stays an empty array, not NULL, so it still blocks inheritance.
  CASE WHEN jsonb_typeof(c->'lendersOfLastResort') = 'array' THEN ARRAY(
    SELECT (lender.value->>'authority') || ':' || (lender.value->>'symbol')
    FROM jsonb_array_elements(c->'lendersOfLastResort') WITH ORDINALITY AS lender (value, position)
    ORDER BY lender.position
  ) END,
  (c->>'includeRequestingAgencyInfo')::boolean, (c->>'includeSupplierInfo')::boolean, (c->>'includeReturnInfo')::boolean,
  (c->>'includeVendorNote')::boolean, (c->>'useOfferedCosts')::boolean,
  c->>'noteFieldSeparator', c->>'supplierPatronPattern', (c->>'duplicateCheckWindowHours')::integer,
  (c->>'defaultLoanPeriod')::integer, (c->>'maxRequestsPerPatron')::integer,
  (c->>'isPickupLocation')::boolean, (c->>'minimumCost')::double precision, c->>'loadBalancingPolicy',
  c->'unfilledReasons', c->'cancellationReasons', c->'loanConditions', c->'availableCopyrightTypes', c->'defaultCopyrightType',
  c->>'requestIdPrefix'
FROM entries e, LATERAL (SELECT e.configs->'illConfig' AS c) config
WHERE e.configs ? 'illConfig';

INSERT INTO lms_configs (
  entry, address, from_agency, from_agency_authentication, to_agency,
  lookup_user_enabled, accept_item_enabled, checkin_item_enabled, checkout_item_enabled, item_location,
  request_item_request_type, request_item_scope_type, request_item_bib_code, request_item_pickup_location_enabled,
  requester_pickup_location, supplier_pickup_location, requester_patron_pattern, request_item_enabled, patron_profiles,
  vendor, ncip_namespace_enabled, bib_id_normalization, ncip_version, bib_item_scope_name
)
SELECT e.id, COALESCE(c->>'address', ''), COALESCE(c->>'fromAgency', ''), c->>'fromAgencyAuthentication', c->>'toAgency',
  (c->>'lookupUserEnabled')::boolean, (c->>'acceptItemEnabled')::boolean, (c->>'checkInItemEnabled')::boolean,
  (c->>'checkOutItemEnabled')::boolean, c->>'itemLocation',
  c->>'requestItemRequestType', c->>'requestItemRequestScopeType', c->>'requestItemBibIdCode',
  (c->>'requestItemPickupLocationEnabled')::boolean,
  c->>'requesterPickupLocation', c->>'supplierPickupLocation', c->>'requesterPatronPattern', (c->>'requestItemEnabled')::boolean,
  c->'patronProfiles',
  c->>'vendor', (c->>'ncipNamespaceEnabled')::boolean, c->>'bibIdNormalization', c->>'ncipVersion', c->>'bibItemScopeName'
FROM entries e, LATERAL (SELECT e.configs->'lmsConfig' AS c) config
WHERE e.configs ? 'lmsConfig';

-- Version 027 stored holdingsFormat in both the holdings_config JSON column
-- and dedicated MARC subfield columns and format flags. Restore both forms.
INSERT INTO catalog_configs (
  entry, metadata_update_mode, sru_address, sru_record_schema, zoom_address,
  query_type, query_identifier, query_isbn, query_issn, query_title,
  holdings_marc_call_number_subfield, holdings_marc_item_id_subfield, holdings_marc_location_subfield,
  holdings_marc_main_field, holdings_marc_restricted_subfield, holdings_marc_shelving_location_subfield,
  holdings_marc21plus1_enabled, holdings_opac_enabled, holdings_reservoir_enabled,
  metadata_marc21_author, metadata_marc21_edition, metadata_marc21_identifier, metadata_marc21_isbn,
  metadata_marc21_issn, metadata_marc21_subtitle, metadata_marc21_title,
  zoom_options, profile, holdings_config, query_year, query_identifier_normalization, query_identifier_prefix,
  metadata_marc21_publisher, metadata_marc21_publication_date, metadata_marc21_place_of_publication
)
SELECT e.id, c->>'metadataUpdateMode', c->'sru'->>'address', c->'sru'->>'recordSchema', c->'zoom'->>'address',
  c->'queryConfig'->>'type', c->'queryConfig'->>'identifier', c->'queryConfig'->>'isbn', c->'queryConfig'->>'issn',
  c->'queryConfig'->>'title',
  c->'holdingsFormat'->'marc'->>'callNumberSubfield', c->'holdingsFormat'->'marc'->>'itemIdSubfield',
  c->'holdingsFormat'->'marc'->>'locationSubfield', c->'holdingsFormat'->'marc'->>'mainField',
  c->'holdingsFormat'->'marc'->>'restrictedSubfield', c->'holdingsFormat'->'marc'->>'shelvingLocationSubfield',
  CASE WHEN c ? 'holdingsFormat' THEN c->'holdingsFormat' ? 'marc21plus1' END,
  CASE WHEN c ? 'holdingsFormat' THEN c->'holdingsFormat' ? 'opac' END,
  CASE WHEN c ? 'holdingsFormat' THEN c->'holdingsFormat' ? 'reservoir' END,
  c->'metadataFormat'->'marc21'->>'author', c->'metadataFormat'->'marc21'->>'edition',
  c->'metadataFormat'->'marc21'->>'identifier', c->'metadataFormat'->'marc21'->>'isbn',
  c->'metadataFormat'->'marc21'->>'issn', c->'metadataFormat'->'marc21'->>'subtitle',
  c->'metadataFormat'->'marc21'->>'title',
  c->'zoom'->'options', c->>'profile', c->'holdingsFormat', c->'queryConfig'->>'year',
  c->'queryConfig'->>'identifierNormalization', c->'queryConfig'->>'identifierPrefix',
  c->'metadataFormat'->'marc21'->>'publisher', c->'metadataFormat'->'marc21'->>'publicationDate',
  c->'metadataFormat'->'marc21'->>'placeOfPublication'
FROM entries e, LATERAL (SELECT e.configs->'catalogConfig' AS c) config
WHERE e.configs ? 'catalogConfig';

INSERT INTO holdings_policies (entry, policy)
SELECT id, configs->'holdingsPolicy' FROM entries WHERE configs ? 'holdingsPolicy';

ALTER TABLE entries DROP COLUMN configs;
