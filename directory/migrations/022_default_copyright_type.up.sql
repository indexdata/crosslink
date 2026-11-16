ALTER TABLE ill_configs
  ADD COLUMN default_copyright_type jsonb CHECK (jsonb_typeof(default_copyright_type) = 'object');
