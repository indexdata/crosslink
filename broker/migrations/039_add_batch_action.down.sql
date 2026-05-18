DROP TABLE IF EXISTS batch_action;

DELETE FROM event_config WHERE event_name = 'send-email';
