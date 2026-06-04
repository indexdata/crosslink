ALTER TABLE batch_action
DROP COLUMN action_params;

INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('send-email', 'TASK', 1)
    ON CONFLICT (event_name) DO NOTHING;
UPDATE scheduled_task SET event_name = 'send-email' WHERE event_name = 'invoke-batch-action';
UPDATE event SET event_name = 'send-email' WHERE event_name = 'invoke-batch-action';
UPDATE batch_action SET action_name = 'email' WHERE action_name = 'invoke-batch-action';

DELETE FROM event_config
WHERE event_name = 'invoke-batch-action';