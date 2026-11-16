-- The previous constraint allows only ASCII letters and digits, so ':' and '-'
-- are removed first: "US-ABC" becomes "USABC". '/' is never stored, but is
-- removed too in case the up constraint was loosened. Every value is at
-- most 16 characters, so the old 32-character limit holds. A value of only
-- punctuation becomes empty, which means "use the requester symbol".
UPDATE ill_configs
SET request_id_prefix = regexp_replace(request_id_prefix, '[/:-]', '', 'g')
WHERE request_id_prefix ~ '[/:-]';
ALTER TABLE ill_configs DROP CONSTRAINT ill_configs_request_id_prefix_check;
ALTER TABLE ill_configs ADD CONSTRAINT ill_configs_request_id_prefix_check
  CHECK (request_id_prefix ~ '^[A-Za-z0-9]{0,32}$');
