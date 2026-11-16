-- Earlier versions treat null as "off" and as "use the profile default".
UPDATE ill_configs SET default_loan_period = NULL WHERE default_loan_period = 0;
ALTER TABLE ill_configs DROP CONSTRAINT ill_configs_default_loan_period_check;
ALTER TABLE ill_configs ADD CONSTRAINT ill_configs_default_loan_period_check CHECK (default_loan_period > 0);
UPDATE lms_configs SET ncip_version = NULL WHERE ncip_version = 'default';
ALTER TABLE lms_configs DROP CONSTRAINT lms_configs_ncip_version_check;
ALTER TABLE lms_configs ADD CONSTRAINT lms_configs_ncip_version_check CHECK (ncip_version IN ('1.0', '2.02'));
