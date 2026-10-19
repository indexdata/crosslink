DROP TABLE holdings_policies;

ALTER TABLE catalog_configs
  ADD COLUMN zoom_option_mock_records text,
  ADD COLUMN zoom_option_preferred_record_syntax text,
  ADD COLUMN zoom_option_count text,
  ADD COLUMN zoom_option_element_set_name text,
  ADD COLUMN zoom_option_schema text,
  ADD COLUMN zoom_option_authentication text,
  ADD COLUMN zoom_option_user text,
  ADD COLUMN zoom_option_password text,
  ADD COLUMN zoom_option_adapter_error text,
  ADD COLUMN zoom_option_lookup_error text,
  ADD COLUMN zoom_option_location text;

UPDATE catalog_configs SET
  zoom_option_mock_records = zoom_options->>'mockRecords',
  zoom_option_preferred_record_syntax = zoom_options->>'preferredRecordSyntax',
  zoom_option_count = zoom_options->>'count',
  zoom_option_element_set_name = zoom_options->>'elementSetName',
  zoom_option_schema = zoom_options->>'schema',
  zoom_option_authentication = zoom_options->>'authentication',
  zoom_option_user = zoom_options->>'user',
  zoom_option_password = zoom_options->>'password',
  zoom_option_adapter_error = zoom_options->>'adapter-error',
  zoom_option_lookup_error = zoom_options->>'lookup-error',
  zoom_option_location = zoom_options->>'location';

ALTER TABLE catalog_configs DROP COLUMN zoom_options;
