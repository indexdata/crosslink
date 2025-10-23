ALTER TABLE event DROP COLUMN patron_request_id;

DROP INDEX IF EXISTS event_patron_request_id_idx;