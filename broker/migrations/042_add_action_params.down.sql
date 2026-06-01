ALTER TABLE batch_action
DROP COLUMN action_params;

UPDATE event_config SET event_name = 'send-email' WHERE event_name = 'email-pullslips';
