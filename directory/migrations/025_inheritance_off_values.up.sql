-- Null now means "inherit from the parent entry". These values store "off" locally.
ALTER TABLE ill_configs DROP CONSTRAINT ill_configs_default_loan_period_check;
ALTER TABLE ill_configs ADD CONSTRAINT ill_configs_default_loan_period_check CHECK (default_loan_period >= 0);
ALTER TABLE lms_configs DROP CONSTRAINT lms_configs_ncip_version_check;
ALTER TABLE lms_configs ADD CONSTRAINT lms_configs_ncip_version_check CHECK (ncip_version IN ('1.0', '2.02', 'default'));
