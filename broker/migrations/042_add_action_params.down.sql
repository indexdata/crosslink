ALTER TABLE batch_action
DROP COLUMN action_params;

INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('send-email', 'TASK', 1)
    ON CONFLICT (event_name) DO NOTHING;
UPDATE scheduled_task SET event_name = 'send-email' WHERE event_name = 'email-pullslips';
UPDATE event SET event_name = 'send-email' WHERE event_name = 'email-pullslips';

DELETE FROM event_config
WHERE event_name = 'email-pullslips';