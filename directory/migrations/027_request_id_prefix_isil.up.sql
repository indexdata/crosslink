-- Request ID prefixes may use ISIL characters: ASCII letters, digits, '/', ':' and '-'.
ALTER TABLE ill_configs DROP CONSTRAINT ill_configs_request_id_prefix_check;
ALTER TABLE ill_configs ADD CONSTRAINT ill_configs_request_id_prefix_check
  CHECK (request_id_prefix ~ '^[A-Za-z0-9/:-]{0,16}$');
