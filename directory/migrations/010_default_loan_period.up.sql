ALTER TABLE ill_configs ADD COLUMN default_loan_period INTEGER CHECK (default_loan_period > 0);
