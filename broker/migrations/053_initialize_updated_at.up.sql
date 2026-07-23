UPDATE patron_request
SET updated_at = created_at
WHERE updated_at IS NULL;

ALTER TABLE patron_request
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL;

UPDATE scheduled_task
SET updated_at = created_at
WHERE updated_at IS NULL;

ALTER TABLE scheduled_task
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL;

UPDATE template
SET updated_at = created_at
WHERE updated_at IS NULL;

ALTER TABLE template
    ALTER COLUMN updated_at SET DEFAULT now(),
    ALTER COLUMN updated_at SET NOT NULL;
