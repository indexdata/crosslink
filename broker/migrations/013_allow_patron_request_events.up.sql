ALTER TABLE event DROP CONSTRAINT event_ill_transaction_id_fkey;

ALTER TABLE event ADD COLUMN patron_request_id  VARCHAR   NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS event_ill_transaction_id_idx ON event (ill_transaction_id);
CREATE INDEX IF NOT EXISTS event_patron_request_id_idx ON event (patron_request_id);