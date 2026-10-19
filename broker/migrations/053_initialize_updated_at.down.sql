ALTER TABLE patron_request
    ALTER COLUMN updated_at DROP NOT NULL,
    ALTER COLUMN updated_at DROP DEFAULT;

ALTER TABLE scheduled_task
    ALTER COLUMN updated_at DROP NOT NULL,
    ALTER COLUMN updated_at DROP DEFAULT;

ALTER TABLE template
    ALTER COLUMN updated_at DROP NOT NULL,
    ALTER COLUMN updated_at DROP DEFAULT;
