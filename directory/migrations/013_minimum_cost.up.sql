ALTER TABLE ill_configs
  ADD COLUMN minimum_cost DOUBLE PRECISION CHECK (minimum_cost >= 0);
