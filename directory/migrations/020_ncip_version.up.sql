ALTER TABLE lms_configs ADD COLUMN ncip_version text CHECK (ncip_version IN ('1.0', '2.02'));
