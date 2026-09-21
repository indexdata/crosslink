UPDATE tiers SET type = 'loan' WHERE type = 'copyorloan';
ALTER TABLE tiers DROP CONSTRAINT tiers_type_check;
ALTER TABLE tiers ADD CONSTRAINT tiers_type_check CHECK (type IN ('loan', 'copy'));
