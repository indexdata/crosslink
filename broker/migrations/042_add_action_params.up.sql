ALTER TABLE batch_action
ADD COLUMN action_params JSONB;

INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('invoke-batch-action', 'TASK', 1)
    ON CONFLICT (event_name) DO NOTHING;
UPDATE scheduled_task SET event_name = 'invoke-batch-action' WHERE event_name = 'send-email';
UPDATE event SET event_name = 'invoke-batch-action' WHERE event_name = 'send-email';
UPDATE batch_action SET action_name = 'invoke-batch-action' WHERE action_name = 'email';

DELETE FROM event_config
WHERE event_name = 'send-email';