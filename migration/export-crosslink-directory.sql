-- Export a mod-rs tenant's directory as CrossLink directory-import NDJSON.
--
-- Run this against one mod-rs tenant schema. The database role/search_path must
-- resolve the tables below to that tenant's schema.
--
-- Example:
--   psql "$DATABASE_URL" \
--     --set=ON_ERROR_STOP=1 \
--     --file=other-scripts/export-crosslink-directory.sql \
--     --quiet --tuples-only --no-align \
--     > crosslink-directory.ndjson
--
-- The output order is significant: parent entries precede their children,
-- followed by default tiers and then the default network.
--
-- Mapping notes:
--   * The first symbol by legacy priority, authority, and value is the stable
--     CrossLink import key for an entry. Deleted entry tombstones and residual
--     DELETED-* symbols created by mod-rs anonymization are omitted.
--   * service/service_account rows become entry endpoints.
--   * address/address_line rows become entry addresses. addr_country_code is
--     appended as a CountryCode component when one is not already present.
--   * Tenant-local NCIP, Z39.50 target, ILL, and holdings settings are attached
--     to the entry identified by default_request_symbol. The legacy HTTP
--     Z39.50 proxy is deployment-level CrossLink configuration and is omitted.
--     Configure last-resort lenders after import because references to child
--     entries cannot be resolved while their parent entry is being imported.
--   * default_service_level and minimum_cost seed loan and copy default tiers.
--     Missing settings become standard and 0. The automatic-fee
--     request_service_type setting does not describe routing capabilities.
--   * Every non-consortium entry belongs to each generated tier and to one
--     reciprocal network named Default.
--   * Fields with no mod-rs equivalent are emitted as explicit nulls or empty
--     arrays because the CrossLink import contract requires every field.
--
-- The output can contain tenant credentials such as NCIP authentication data.
-- Store and transfer it as sensitive migration material.

\set ON_ERROR_STOP on

BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ;

CREATE TEMP TABLE crosslink_entry_base ON COMMIT DROP AS
SELECT
    directory_entry.de_id AS entry_id,
    directory_entry.de_parent AS parent_id,
    directory_entry.de_name AS name,
    CASE lower(btrim(entry_type.rdv_value))
        WHEN 'consortium' THEN 'Consortium'
        WHEN 'institution' THEN 'Institution'
        WHEN 'branch' THEN 'Branch'
        ELSE NULL
    END AS entry_type,
    directory_entry.de_desc AS description,
    directory_entry.de_contact_name AS contact_name,
    directory_entry.de_email_address AS email,
    directory_entry.de_phone_number AS phone_number,
    directory_entry.de_lms_location_code AS lms_location_code
FROM directory_entry
LEFT JOIN refdata_value AS entry_type
  ON entry_type.rdv_id = directory_entry.de_type_rv_fk
WHERE NOT EXISTS (
    SELECT 1
    FROM directory_entry_tag
    JOIN tag ON tag.id = directory_entry_tag.tag_id
    WHERE directory_entry_tag.directory_entry_tags_id = directory_entry.de_id
      AND lower(btrim(tag.norm_value)) = 'deleted'
);

CREATE TEMP TABLE crosslink_symbols ON COMMIT DROP AS
SELECT
    symbol.sym_owner_fk AS entry_id,
    upper(btrim(naming_authority.na_symbol)) AS authority,
    upper(btrim(symbol.sym_symbol)) AS symbol,
    row_number() OVER (
        PARTITION BY symbol.sym_owner_fk
        ORDER BY symbol.sym_priority NULLS LAST,
                 upper(btrim(naming_authority.na_symbol)),
                 upper(btrim(symbol.sym_symbol)),
                 symbol.sym_id
    ) AS key_order
FROM symbol
JOIN naming_authority
  ON naming_authority.na_id = symbol.sym_authority_fk
JOIN crosslink_entry_base
  ON crosslink_entry_base.entry_id = symbol.sym_owner_fk
WHERE nullif(btrim(naming_authority.na_symbol), '') IS NOT NULL
  AND nullif(btrim(symbol.sym_symbol), '') IS NOT NULL
  AND upper(btrim(symbol.sym_symbol)) NOT LIKE 'DELETED-%';

CREATE TEMP TABLE crosslink_entry_keys ON COMMIT DROP AS
SELECT entry_id, authority, symbol
FROM crosslink_symbols
WHERE key_order = 1;

CREATE TEMP TABLE crosslink_hierarchy ON COMMIT DROP AS
WITH RECURSIVE hierarchy AS (
    SELECT
        entry.entry_id,
        entry.parent_id,
        0 AS depth,
        ARRAY[entry.entry_id]::text[] AS entry_path
    FROM crosslink_entry_base AS entry
    WHERE entry.parent_id IS NULL

    UNION ALL

    SELECT
        child.entry_id,
        child.parent_id,
        parent.depth + 1,
        parent.entry_path || child.entry_id
    FROM hierarchy AS parent
    JOIN crosslink_entry_base AS child
      ON child.parent_id = parent.entry_id
    WHERE NOT child.entry_id = ANY(parent.entry_path)
)
SELECT * FROM hierarchy;

CREATE TEMP TABLE crosslink_tier_settings ON COMMIT DROP AS
WITH settings AS (
    SELECT
        max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
            FILTER (WHERE st_key = 'default_service_level') AS service_level,
        max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
            FILTER (WHERE st_key = 'minimum_cost') AS minimum_cost
    FROM app_setting
)
SELECT
    coalesce(lower(service_level), 'standard') AS service_level,
    coalesce(minimum_cost, '0') AS minimum_cost
FROM settings;

CREATE TEMP TABLE crosslink_tenant_settings ON COMMIT DROP AS
SELECT
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'default_request_symbol') AS default_request_symbol,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'host_lms_integration') AS host_lms_integration,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'ncip_server_address') AS ncip_server_address,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'ncip_from_agency') AS ncip_from_agency,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'ncip_from_agency_authentication') AS ncip_from_agency_authentication,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'ncip_to_agency') AS ncip_to_agency,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'borrower_check') AS borrower_check,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'accept_item') AS accept_item,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'check_in_item') AS check_in_item,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'check_out_item') AS check_out_item,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'use_request_item') AS use_request_item,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'ncip_request_item_pickup_location') AS requester_pickup_location,
    max(coalesce(nullif(btrim(st_value), ''), nullif(btrim(st_default_value), '')))
        FILTER (WHERE st_key = 'z3950_server_address') AS z3950_server_address
FROM app_setting;

CREATE TEMP TABLE crosslink_local_entry ON COMMIT DROP AS
SELECT DISTINCT symbol.entry_id
FROM crosslink_symbols AS symbol
CROSS JOIN crosslink_tenant_settings AS settings
WHERE symbol.authority || ':' || symbol.symbol = upper(btrim(settings.default_request_symbol));

DO $$
DECLARE
    problem text;
    consortium_count integer;
    entry_count integer;
    hierarchy_count integer;
    minimum_cost_text text;
    service_level_text text;
    local_entry_count integer;
    ncip_server_text text;
    ncip_from_agency_text text;
    ncip_dependent_config boolean;
BEGIN
    SELECT count(*) INTO consortium_count
    FROM crosslink_entry_base
    WHERE entry_type = 'Consortium';
    IF consortium_count <> 1 THEN
        RAISE EXCEPTION 'CrossLink directory export requires exactly one Consortium entry; found %', consortium_count;
    END IF;

    SELECT string_agg(entry_id || ' (' || coalesce(name, 'NULL') || ')', ', ' ORDER BY entry_id)
    INTO problem
    FROM crosslink_entry_base
    WHERE nullif(btrim(name), '') IS NULL;
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Directory entries need nonblank names: %', problem;
    END IF;

    SELECT string_agg(entry_id || ' (' || name || ')', ', ' ORDER BY entry_id)
    INTO problem
    FROM crosslink_entry_base
    WHERE entry_type IS NULL;
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Directory entries have unsupported or missing types: %', problem;
    END IF;

    SELECT string_agg(entry.entry_id || ' (' || entry.name || ')', ', ' ORDER BY entry.entry_id)
    INTO problem
    FROM crosslink_entry_base AS entry
    LEFT JOIN crosslink_entry_keys AS key ON key.entry_id = entry.entry_id
    WHERE key.entry_id IS NULL;
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Every directory entry needs a nonblank symbol: %', problem;
    END IF;

    SELECT string_agg(authority || ':' || symbol, ', ' ORDER BY authority, symbol)
    INTO problem
    FROM (
        SELECT authority, symbol
        FROM crosslink_symbols
        GROUP BY authority, symbol
        HAVING count(*) > 1
    ) AS duplicate_symbol;
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Symbols are not unique after trimming and uppercasing: %', problem;
    END IF;

    SELECT count(*) INTO entry_count FROM crosslink_entry_base;
    SELECT count(*) INTO hierarchy_count FROM crosslink_hierarchy;
    IF hierarchy_count <> entry_count THEN
        SELECT string_agg(entry.entry_id || ' (' || entry.name || ')', ', ' ORDER BY entry.entry_id)
        INTO problem
        FROM crosslink_entry_base AS entry
        LEFT JOIN crosslink_hierarchy AS hierarchy USING (entry_id)
        WHERE hierarchy.entry_id IS NULL;
        RAISE EXCEPTION 'Directory hierarchy contains a cycle or an unreachable parent: %', problem;
    END IF;

    SELECT string_agg(child.entry_id || ' (' || child.name || ')', ', ' ORDER BY child.entry_id)
    INTO problem
    FROM crosslink_entry_base AS child
    LEFT JOIN crosslink_entry_base AS parent ON parent.entry_id = child.parent_id
    WHERE (child.entry_type = 'Consortium' AND child.parent_id IS NOT NULL)
       OR (child.entry_type = 'Institution' AND child.parent_id IS NOT NULL AND parent.entry_type <> 'Consortium')
       OR (child.entry_type = 'Branch' AND (child.parent_id IS NULL OR parent.entry_type <> 'Institution'));
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Directory entries have parent relationships rejected by CrossLink: %', problem;
    END IF;

    SELECT string_agg(service.se_id, ', ' ORDER BY service.se_id)
    INTO problem
    FROM service
    JOIN service_account ON service_account.sa_service = service.se_id
    JOIN crosslink_entry_base ON crosslink_entry_base.entry_id = service_account.sa_account_holder
    LEFT JOIN refdata_value AS service_type ON service_type.rdv_id = service.se_type_fk
    WHERE nullif(btrim(service.se_name), '') IS NULL
       OR nullif(btrim(service.se_address), '') IS NULL
       OR nullif(btrim(service_type.rdv_value), '') IS NULL;
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Directory services cannot become CrossLink endpoints because required values are blank: %', problem;
    END IF;

    SELECT string_agg(st_key || ' (' || setting_count || ' rows)', ', ' ORDER BY st_key)
    INTO problem
    FROM (
        SELECT st_key, count(*) AS setting_count
        FROM app_setting
        WHERE st_key IN (
            'default_service_level', 'minimum_cost', 'default_request_symbol',
            'host_lms_integration', 'ncip_server_address', 'ncip_from_agency',
            'ncip_from_agency_authentication', 'ncip_to_agency', 'borrower_check',
            'accept_item', 'check_in_item', 'check_out_item', 'use_request_item',
            'ncip_request_item_pickup_location', 'z3950_server_address'
        )
        GROUP BY st_key
        HAVING count(*) > 1
    ) AS duplicate_setting;
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Export settings must have at most one row per key: %', problem;
    END IF;

    SELECT service_level, minimum_cost
    INTO service_level_text, minimum_cost_text
    FROM crosslink_tier_settings;
    IF service_level_text NOT IN ('express', 'normal', 'rush', 'secondarymail', 'standard', 'urgent') THEN
        RAISE EXCEPTION 'Unsupported default_service_level for CrossLink tier: %', service_level_text;
    END IF;
    IF minimum_cost_text !~ '^(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$' THEN
        RAISE EXCEPTION 'minimum_cost must be a nonnegative number for CrossLink tier export: %', minimum_cost_text;
    END IF;

    SELECT string_agg(source || ':' || record_id || '=' || supply_preference, ', ' ORDER BY source, record_id)
    INTO problem
    FROM (
        SELECT 'location' AS source, hll_id::text AS record_id,
               hll_supply_preference::text AS supply_preference
        FROM host_lms_location
        WHERE hll_supply_preference > 10000
          AND coalesce(hll_hidden, false) IS FALSE

        UNION ALL

        SELECT 'shelving-location', hlsl_id::text, hlsl_supply_preference::text
        FROM host_lms_shelving_loc
        WHERE hlsl_supply_preference > 10000
          AND coalesce(hlsl_hidden, false) IS FALSE

        UNION ALL

        SELECT 'location-policy', site.sls_id::text, site.sls_supply_preference::text
        FROM shelving_loc_site AS site
        JOIN host_lms_location AS location ON location.hll_id = site.sls_location_fk
        JOIN host_lms_shelving_loc AS shelving ON shelving.hlsl_id = site.sls_shelving_loc_fk
        WHERE site.sls_supply_preference > 10000
          AND coalesce(location.hll_hidden, false) IS FALSE
          AND coalesce(shelving.hlsl_hidden, false) IS FALSE
    ) AS invalid_preference;
    IF problem IS NOT NULL THEN
        RAISE EXCEPTION 'Holdings supply preferences must not exceed 10000: %', problem;
    END IF;

    SELECT
        ncip_server_address,
        ncip_from_agency,
        ncip_from_agency_authentication IS NOT NULL
            OR ncip_to_agency IS NOT NULL
            OR lower(borrower_check) = 'ncip'
            OR lower(accept_item) = 'ncip'
            OR lower(check_in_item) = 'ncip'
            OR lower(check_out_item) = 'ncip'
            OR lower(use_request_item) = 'ncip'
            OR requester_pickup_location IS NOT NULL
    INTO ncip_server_text, ncip_from_agency_text, ncip_dependent_config
    FROM crosslink_tenant_settings;
    IF (ncip_server_text IS NULL) <> (ncip_from_agency_text IS NULL)
       OR (ncip_server_text IS NULL AND coalesce(ncip_dependent_config, false)) THEN
        RAISE EXCEPTION 'NCIP export requires both ncip_server_address and ncip_from_agency when NCIP configuration is present';
    END IF;

    SELECT count(*) INTO local_entry_count FROM crosslink_local_entry;
    IF local_entry_count <> 1 THEN
        RAISE EXCEPTION 'default_request_symbol must identify exactly one exported directory entry; found %', local_entry_count;
    END IF;

END
$$;

CREATE TEMP TABLE crosslink_export_records ON COMMIT DROP AS
WITH ordered_entries AS (
    SELECT
        entry.*,
        hierarchy.depth,
        key.authority AS key_authority,
        key.symbol AS key_symbol,
        row_number() OVER (
            ORDER BY hierarchy.depth, entry.name, entry.entry_id
        ) AS entry_order
    FROM crosslink_entry_base AS entry
    JOIN crosslink_hierarchy AS hierarchy USING (entry_id)
    JOIN crosslink_entry_keys AS key USING (entry_id)
),
entry_records AS (
    SELECT
        1 AS record_type_order,
        entry.entry_order AS record_order,
        jsonb_build_object(
            'type', 'entry',
            'key', jsonb_build_object(
                'authority', entry.key_authority,
                'symbol', entry.key_symbol
            ),
            'data', jsonb_build_object(
                'name', entry.name,
                'type', entry.entry_type,
                'parent', CASE WHEN parent_key.entry_id IS NULL THEN NULL ELSE jsonb_build_object(
                    'authority', parent_key.authority,
                    'symbol', parent_key.symbol
                ) END,
                'description', entry.description,
                'organizationId', NULL,
                'contactName', entry.contact_name,
                'email', entry.email,
                'fromEmail', NULL,
                'tenant', NULL,
                'vendor', CASE WHEN local_entry.entry_id IS NOT NULL THEN 'ReShare' ELSE NULL END,
                'phoneNumber', entry.phone_number,
                'lmsLocationCode', entry.lms_location_code,
                'hrid', NULL,
                'timeZone', NULL,
                'symbols', coalesce(symbols.items, '[]'::jsonb),
                'endpoints', coalesce(endpoints.items, '[]'::jsonb),
                'addresses', coalesce(addresses.items, '[]'::jsonb),
                'closures', '[]'::jsonb,
                'lmsConfig', lms_config.item,
                'catalogConfig', catalog_config.item,
                'illConfig', ill_config.item,
                'holdingsPolicy', holdings_policy.item
            )
        ) AS record
    FROM ordered_entries AS entry
    LEFT JOIN crosslink_entry_keys AS parent_key ON parent_key.entry_id = entry.parent_id
    LEFT JOIN LATERAL (
        SELECT jsonb_agg(
            jsonb_build_object('authority', authority, 'symbol', symbol)
            ORDER BY key_order
        ) AS items
        FROM crosslink_symbols
        WHERE entry_id = entry.entry_id
    ) AS symbols ON true
    LEFT JOIN LATERAL (
        SELECT jsonb_agg(endpoint ORDER BY endpoint ->> 'name', endpoint ->> 'type', endpoint ->> 'address') AS items
        FROM (
            SELECT DISTINCT jsonb_build_object(
                'name', service.se_name,
                'type', service_type.rdv_value,
                'address', service.se_address
            ) AS endpoint
            FROM service_account
            JOIN service ON service.se_id = service_account.sa_service
            JOIN refdata_value AS service_type ON service_type.rdv_id = service.se_type_fk
            WHERE service_account.sa_account_holder = entry.entry_id
        ) AS distinct_endpoints
    ) AS endpoints ON true
    LEFT JOIN LATERAL (
        SELECT jsonb_agg(address_record ORDER BY address_order) AS items
        FROM (
            SELECT
                address.addr_id AS address_order,
                jsonb_build_object(
                    'type', CASE lower(btrim(address.addr_label))
                        WHEN 'default' THEN 'Default'
                        WHEN 'shipping' THEN 'Shipping'
                        WHEN 'billing' THEN 'Billing'
                        WHEN 'other' THEN 'Other'
                        ELSE 'Other'
                    END,
                    'addressComponents', coalesce(components.items, '[]'::jsonb)
                ) AS address_record
            FROM address
            LEFT JOIN LATERAL (
                SELECT jsonb_agg(
                    jsonb_build_object('seq', component.seq, 'type', component.type, 'value', component.value)
                    ORDER BY component.seq, component.component_order
                ) AS items
                FROM (
                    SELECT
                        address_line.al_seq::integer AS seq,
                        address_line.al_id AS component_order,
                        CASE lower(btrim(address_line_type.rdv_value))
                            WHEN 'thoroughfare' THEN 'Thoroughfare'
                            WHEN 'locality' THEN 'Locality'
                            WHEN 'administrativearea' THEN 'AdministrativeArea'
                            WHEN 'postalcode' THEN 'PostalCode'
                            WHEN 'countrycode' THEN 'CountryCode'
                            ELSE 'Other'
                        END AS type,
                        address_line.al_value AS value
                    FROM address_line
                    JOIN refdata_value AS address_line_type
                      ON address_line_type.rdv_id = address_line.al_type_rv_fk
                    WHERE address_line.owner_id = address.addr_id

                    UNION ALL

                    SELECT
                        coalesce(max(address_line.al_seq), 0)::integer + 1 AS seq,
                        '~country-code' AS component_order,
                        'CountryCode' AS type,
                        address.addr_country_code AS value
                    FROM address_line
                    LEFT JOIN refdata_value AS address_line_type
                      ON address_line_type.rdv_id = address_line.al_type_rv_fk
                    WHERE address_line.owner_id = address.addr_id
                    HAVING nullif(btrim(address.addr_country_code), '') IS NOT NULL
                       AND count(*) FILTER (
                           WHERE lower(btrim(address_line_type.rdv_value)) = 'countrycode'
                       ) = 0
                ) AS component
            ) AS components ON true
            WHERE address.owner_id = entry.entry_id
        ) AS entry_addresses
    ) AS addresses ON true
    LEFT JOIN crosslink_local_entry AS local_entry ON local_entry.entry_id = entry.entry_id
    CROSS JOIN crosslink_tenant_settings AS tenant_settings
    LEFT JOIN LATERAL (
        SELECT jsonb_build_object(
            'vendor', CASE lower(tenant_settings.host_lms_integration)
                WHEN 'alma' THEN 'Alma'
                WHEN 'sierra' THEN 'Sierra'
                WHEN 'koha' THEN 'Koha'
                WHEN 'folio' THEN 'FOLIO'
                WHEN 'wms' THEN 'WMS'
                WHEN 'wms2' THEN 'WMS'
                WHEN 'aleph' THEN 'Aleph'
                ELSE 'Generic'
            END,
            'ncipNamespaceEnabled', NULL,
            'bibIdNormalization', NULL,
            'address', tenant_settings.ncip_server_address,
            'fromAgency', tenant_settings.ncip_from_agency,
            'fromAgencyAuthentication', tenant_settings.ncip_from_agency_authentication,
            'toAgency', tenant_settings.ncip_to_agency,
            'lookupUserEnabled', lower(tenant_settings.borrower_check) = 'ncip',
            'acceptItemEnabled', lower(tenant_settings.accept_item) = 'ncip',
            'checkInItemEnabled', lower(tenant_settings.check_in_item) = 'ncip',
            'checkOutItemEnabled', lower(tenant_settings.check_out_item) = 'ncip',
            'itemLocation', NULL,
            'requestItemRequestType', NULL,
            'requestItemRequestScopeType', NULL,
            'requestItemBibIdCode', NULL,
            'requestItemEnabled', lower(tenant_settings.use_request_item) = 'ncip',
            'requestItemPickupLocationEnabled', tenant_settings.requester_pickup_location IS NOT NULL,
            'requesterPickupLocation', tenant_settings.requester_pickup_location,
            'supplierPickupLocation', NULL,
            'requesterPatronPattern', NULL,
            'patronProfiles', NULL
        ) AS item
        WHERE local_entry.entry_id IS NOT NULL
          AND tenant_settings.ncip_server_address IS NOT NULL
          AND tenant_settings.ncip_from_agency IS NOT NULL
    ) AS lms_config ON true
    LEFT JOIN LATERAL (
        SELECT jsonb_build_object(
            'profile', CASE lower(tenant_settings.host_lms_integration)
                WHEN 'alma' THEN 'Alma'
                WHEN 'sierra' THEN 'Sierra'
                WHEN 'koha' THEN 'Koha'
                WHEN 'folio' THEN 'FOLIO'
                WHEN 'wms' THEN 'WMS'
                WHEN 'wms2' THEN 'WMS'
                WHEN 'aleph' THEN 'Aleph'
                ELSE 'Generic'
            END,
            'metadataUpdateMode', NULL,
            'sru', NULL,
            'zoom', jsonb_build_object(
                'address', tenant_settings.z3950_server_address,
                'options', NULL
            ),
            'queryConfig', NULL,
            'holdingsFormat', NULL,
            'metadataFormat', NULL
        ) AS item
        WHERE local_entry.entry_id IS NOT NULL
          AND tenant_settings.z3950_server_address IS NOT NULL
    ) AS catalog_config ON true
    LEFT JOIN LATERAL (
        SELECT jsonb_build_object(
            'iso18626Url', iso_endpoint.address,
            'iso18626Vendor', CASE WHEN iso_endpoint.address IS NULL THEN NULL ELSE 'ReShare' END,
            'lendersOfLastResort', '[]'::jsonb,
            'includeRequestingAgencyInfo', NULL,
            'includeSupplierInfo', NULL,
            'includeReturnInfo', NULL,
            'includeVendorNote', NULL,
            'useOfferedCosts', NULL,
            'noteFieldSeparator', NULL,
            'supplierPatronPattern', NULL,
            'duplicateCheckWindowHours', NULL
        ) AS item
        FROM (
            SELECT service.se_address AS address
            FROM service_account
            JOIN service ON service.se_id = service_account.sa_service
            JOIN refdata_value AS service_type ON service_type.rdv_id = service.se_type_fk
            WHERE service_account.sa_account_holder = entry.entry_id
              AND upper(btrim(service_type.rdv_value)) = 'ISO18626'
            ORDER BY service.se_id
            LIMIT 1
        ) AS iso_endpoint
        WHERE local_entry.entry_id IS NOT NULL
    ) AS ill_config ON true
    LEFT JOIN LATERAL (
        SELECT jsonb_build_object(
            'locations', coalesce((
                SELECT jsonb_agg(jsonb_build_object(
                    'code', location.hll_code,
                    'name', coalesce(location.hll_name, location.hll_code),
                    'supplyPreference', CASE
                        WHEN location.hll_supply_preference < 0 THEN -1
                        ELSE coalesce(location.hll_supply_preference, 0)::integer
                    END
                ) ORDER BY location.hll_code)
                FROM host_lms_location AS location
                WHERE coalesce(location.hll_hidden, false) IS FALSE
            ), '[]'::jsonb),
            'shelvingLocations', coalesce((
                SELECT jsonb_agg(jsonb_build_object(
                    'code', shelving.hlsl_code,
                    'name', coalesce(shelving.hlsl_name, shelving.hlsl_code),
                    'supplyPreference', CASE
                        WHEN shelving.hlsl_supply_preference < 0 THEN -1
                        ELSE coalesce(shelving.hlsl_supply_preference, 0)::integer
                    END
                ) ORDER BY shelving.hlsl_code)
                FROM host_lms_shelving_loc AS shelving
                WHERE coalesce(shelving.hlsl_hidden, false) IS FALSE
            ), '[]'::jsonb),
            'locationPolicies', coalesce((
                SELECT jsonb_agg(jsonb_build_object(
                    'locationCode', location.hll_code,
                    'shelvingLocationCode', shelving.hlsl_code,
                    'supplyPreference', CASE
                        WHEN site.sls_supply_preference < 0 THEN -1
                        ELSE coalesce(site.sls_supply_preference, 0)::integer
                    END
                ) ORDER BY location.hll_code, shelving.hlsl_code)
                FROM shelving_loc_site AS site
                JOIN host_lms_location AS location ON location.hll_id = site.sls_location_fk
                JOIN host_lms_shelving_loc AS shelving ON shelving.hlsl_id = site.sls_shelving_loc_fk
                WHERE coalesce(location.hll_hidden, false) IS FALSE
                  AND coalesce(shelving.hlsl_hidden, false) IS FALSE
            ), '[]'::jsonb),
            'itemLoanPolicies', coalesce((
                SELECT jsonb_agg(jsonb_build_object(
                    'code', policy.hlilp_code,
                    'name', coalesce(policy.hlilp_name, policy.hlilp_code),
                    'lendable', policy.hlilp_lendable
                ) ORDER BY policy.hlilp_code)
                FROM host_lms_item_loan_policy AS policy
                WHERE policy.hlilp_hidden IS FALSE
            ), '[]'::jsonb)
        ) AS item
        WHERE local_entry.entry_id IS NOT NULL
    ) AS holdings_policy ON true
),
consortium AS (
    SELECT key.authority, key.symbol
    FROM ordered_entries AS entry
    JOIN crosslink_entry_keys AS key USING (entry_id)
    WHERE entry.entry_type = 'Consortium'
),
members AS (
    SELECT
        entry.entry_order,
        entry.key_authority AS authority,
        entry.key_symbol AS symbol,
        row_number() OVER (ORDER BY entry.entry_order)::integer AS priority
    FROM ordered_entries AS entry
    WHERE entry.entry_type <> 'Consortium'
),
tier_types AS (
    SELECT 'loan'::text AS tier_type

    UNION ALL

    SELECT 'copy'::text AS tier_type
),
tier_records AS (
    SELECT
        2 AS record_type_order,
        row_number() OVER (ORDER BY tier_type.tier_type) AS record_order,
        jsonb_build_object(
            'type', 'tier',
            'key', jsonb_build_object(
                'consortium', jsonb_build_object(
                    'authority', consortium.authority,
                    'symbol', consortium.symbol
                ),
                'name', 'Default ' || settings.service_level || ' ' || tier_type.tier_type
            ),
            'data', jsonb_build_object(
                'level', settings.service_level,
                'type', tier_type.tier_type,
                'cost', settings.minimum_cost::double precision,
                'entries', coalesce((
                    SELECT jsonb_agg(
                        jsonb_build_object('authority', member.authority, 'symbol', member.symbol)
                        ORDER BY member.entry_order
                    )
                    FROM members AS member
                ), '[]'::jsonb)
            )
        ) AS record
    FROM crosslink_tier_settings AS settings
    CROSS JOIN tier_types AS tier_type
    CROSS JOIN consortium
),
network_record AS (
    SELECT
        3 AS record_type_order,
        1::bigint AS record_order,
        jsonb_build_object(
            'type', 'network',
            'key', jsonb_build_object(
                'consortium', jsonb_build_object(
                    'authority', consortium.authority,
                    'symbol', consortium.symbol
                ),
                'name', 'Default'
            ),
            'data', jsonb_build_object(
                'reciprocal', true,
                'entries', coalesce((
                    SELECT jsonb_agg(
                        jsonb_build_object(
                            'authority', member.authority,
                            'symbol', member.symbol,
                            'priority', member.priority
                        )
                        ORDER BY member.entry_order
                    )
                    FROM members AS member
                ), '[]'::jsonb)
            )
        ) AS record
    FROM consortium
),
export_records AS (
    SELECT record_type_order, record_order, record FROM entry_records
    UNION ALL
    SELECT record_type_order, record_order, record FROM tier_records
    UNION ALL
    SELECT record_type_order, record_order, record FROM network_record
)
SELECT record_type_order, record_order, record
FROM export_records;

DO $$
DECLARE
    oversized_records text;
BEGIN
    SELECT string_agg(
        record_type_order || ':' || record_order || ' (' || octet_length(record::text) || ' bytes)',
        ', ' ORDER BY record_type_order, record_order
    )
    INTO oversized_records
    FROM crosslink_export_records
    WHERE octet_length(record::text) > 1048576;

    IF oversized_records IS NOT NULL THEN
        RAISE EXCEPTION 'CrossLink directory import records exceed the 1 MiB limit: %', oversized_records;
    END IF;
END
$$;

SELECT record::text
FROM crosslink_export_records
ORDER BY record_type_order, record_order;

COMMIT;
