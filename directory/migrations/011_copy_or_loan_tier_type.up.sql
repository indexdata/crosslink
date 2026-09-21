ALTER TABLE tiers DROP CONSTRAINT tiers_type_check;
ALTER TABLE tiers ADD CONSTRAINT tiers_type_check CHECK (type IN ('loan', 'copy', 'copyorloan'));
