ALTER TABLE catalog_configs
  ADD COLUMN query_identifier_normalization TEXT,
  ADD COLUMN query_identifier_prefix TEXT;
