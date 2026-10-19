ALTER TABLE ill_configs ADD COLUMN load_balancing_policy text
  CHECK (load_balancing_policy IN ('deficit', 'proportional'));
