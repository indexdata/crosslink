ALTER TABLE batch_action
ADD COLUMN action_params JSONB;

UPDATE event_config SET event_name = 'email-pullslips' WHERE event_name = 'send-email';
