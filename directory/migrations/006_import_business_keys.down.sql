ALTER TABLE networks DROP CONSTRAINT networks_consortium_name_unique;
ALTER TABLE tiers DROP CONSTRAINT tiers_consortium_name_unique;

ALTER TABLE networks ALTER COLUMN name DROP NOT NULL;
ALTER TABLE tiers ALTER COLUMN name DROP NOT NULL;
