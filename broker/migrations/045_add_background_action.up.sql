INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('invoke-background-action', 'TASK', 1)
ON CONFLICT (event_name) DO NOTHING;

ALTER TABLE scheduled_task
    ADD COLUMN IF NOT EXISTS title TEXT;
