ALTER TABLE catalog_configs ADD COLUMN zoom_options jsonb;

UPDATE catalog_configs
SET zoom_options = jsonb_strip_nulls(jsonb_build_object(
  'mockRecords', zoom_option_mock_records,
  'preferredRecordSyntax', zoom_option_preferred_record_syntax,
  'count', zoom_option_count,
  'elementSetName', zoom_option_element_set_name,
  'schema', zoom_option_schema,
  'authentication', zoom_option_authentication,
  'user', zoom_option_user,
  'password', zoom_option_password,
  'adapter-error', zoom_option_adapter_error,
  'lookup-error', zoom_option_lookup_error,
  'location', zoom_option_location
));

UPDATE catalog_configs SET zoom_options = NULL WHERE zoom_options = '{}'::jsonb;

ALTER TABLE catalog_configs
  DROP COLUMN zoom_option_mock_records,
  DROP COLUMN zoom_option_preferred_record_syntax,
  DROP COLUMN zoom_option_count,
  DROP COLUMN zoom_option_element_set_name,
  DROP COLUMN zoom_option_schema,
  DROP COLUMN zoom_option_authentication,
  DROP COLUMN zoom_option_user,
  DROP COLUMN zoom_option_password,
  DROP COLUMN zoom_option_adapter_error,
  DROP COLUMN zoom_option_lookup_error,
  DROP COLUMN zoom_option_location;

CREATE TABLE holdings_policies (
  id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  entry uuid NOT NULL UNIQUE REFERENCES entries (id) ON DELETE CASCADE,
  policy jsonb NOT NULL
);
