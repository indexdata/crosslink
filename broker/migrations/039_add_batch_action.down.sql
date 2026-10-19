DROP TABLE IF EXISTS batch_action;

DELETE FROM scheduled_task WHERE event_name = 'send-email';
DELETE FROM event_config WHERE event_name = 'send-email';
