-- Each entry stores its configuration containers as JSON objects, in the form
-- entry reads return them. NULL means the entry stores no such container.
ALTER TABLE entries
  ADD COLUMN ill_config jsonb CONSTRAINT entries_ill_config_object CHECK (jsonb_typeof(ill_config) = 'object'),
  ADD COLUMN lms_config jsonb CONSTRAINT entries_lms_config_object CHECK (jsonb_typeof(lms_config) = 'object'),
  ADD COLUMN catalog_config jsonb CONSTRAINT entries_catalog_config_object CHECK (jsonb_typeof(catalog_config) = 'object'),
  ADD COLUMN holdings_policy jsonb CONSTRAINT entries_holdings_policy_object CHECK (jsonb_typeof(holdings_policy) = 'object');

UPDATE entries e SET ill_config = (
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
		)) FROM ill_configs i WHERE i.entry = e.id)::jsonb
WHERE EXISTS (SELECT 1 FROM ill_configs i WHERE i.entry = e.id);

UPDATE entries e SET lms_config = (
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
		from lms_configs l WHERE l.entry = e.id)::jsonb
WHERE EXISTS (SELECT 1 FROM lms_configs l WHERE l.entry = e.id);

UPDATE entries e SET catalog_config = (
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
							'callNumberSubfield', h.holdings_marc_call_number_subfield,
							'itemIdSubfield', h.holdings_marc_item_id_subfield,
							'locationSubfield', h.holdings_marc_location_subfield,
							'mainField', h.holdings_marc_main_field,
							'restrictedSubfield', h.holdings_marc_restricted_subfield,
							'shelvingLocationSubfield', h.holdings_marc_shelving_location_subfield
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
		from catalog_configs h WHERE h.entry = e.id)::jsonb
WHERE EXISTS (SELECT 1 FROM catalog_configs h WHERE h.entry = e.id);

UPDATE entries e SET holdings_policy = (SELECT jsonb_strip_nulls(hp.policy) FROM holdings_policies hp WHERE hp.entry = e.id)
WHERE EXISTS (SELECT 1 FROM holdings_policies hp WHERE hp.entry = e.id);

DROP TABLE ill_configs, lms_configs, catalog_configs, holdings_policies;
