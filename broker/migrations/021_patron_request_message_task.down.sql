UPDATE event_config
SET event_type = 'NOTICE'
WHERE event_name = 'patron-request-message';
