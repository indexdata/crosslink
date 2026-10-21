ALTER TABLE ill_configs
  ADD COLUMN unfilled_reasons jsonb CHECK (jsonb_typeof(unfilled_reasons) = 'array'),
  ADD COLUMN cancellation_reasons jsonb CHECK (jsonb_typeof(cancellation_reasons) = 'array'),
  ADD COLUMN loan_conditions jsonb CHECK (jsonb_typeof(loan_conditions) = 'array');
