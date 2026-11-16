-- Request ID prefixes may use the ISIL characters ':' and '-' besides ASCII
-- letters and digits. '/' is excluded because it would put slashes into
-- request IDs used as URL path segments.
ALTER TABLE ill_configs DROP CONSTRAINT ill_configs_request_id_prefix_check;
ALTER TABLE ill_configs ADD CONSTRAINT ill_configs_request_id_prefix_check
  CHECK (request_id_prefix ~ '^[A-Za-z0-9:-]{0,16}$');
