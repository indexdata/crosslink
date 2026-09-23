ALTER TABLE ill_configs
  ADD COLUMN max_requests_per_patron INTEGER CHECK (max_requests_per_patron >= 0);
