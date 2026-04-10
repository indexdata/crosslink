UPDATE peer
SET custom_data = '{}'::jsonb
WHERE custom_data IS NULL;

ALTER TABLE peer
    ALTER COLUMN custom_data SET DEFAULT '{}'::jsonb,
    ALTER COLUMN custom_data SET NOT NULL;
