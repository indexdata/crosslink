ALTER TABLE lms_configs ADD COLUMN vendor text;
ALTER TABLE lms_configs ADD COLUMN ncip_namespace_enabled boolean;
ALTER TABLE lms_configs ADD COLUMN bib_id_normalization text;
ALTER TABLE catalog_configs ADD COLUMN profile text;
ALTER TABLE catalog_configs ADD COLUMN holdings_config jsonb;

-- Preserve existing administrator parser settings for subsequent partial PATCHes.
UPDATE catalog_configs h SET holdings_config = NULLIF((json_strip_nulls(json_build_object(
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
				)))::jsonb, '{}'::jsonb);
