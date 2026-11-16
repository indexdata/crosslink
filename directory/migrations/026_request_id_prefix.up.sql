ALTER TABLE ill_configs
  ADD COLUMN request_id_prefix text CHECK (request_id_prefix ~ '^[A-Za-z0-9]{0,32}$');
