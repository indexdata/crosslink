ALTER TABLE event DROP COLUMN patron_request_id;

DELETE FROM event_config WHERE event_name ='invoke-action';