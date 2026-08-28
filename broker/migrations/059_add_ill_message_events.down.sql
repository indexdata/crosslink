DELETE FROM event
WHERE event_name IN ('ill-requester-message', 'ill-supplier-message');

DELETE FROM event_config
WHERE event_name IN ('ill-requester-message', 'ill-supplier-message');
