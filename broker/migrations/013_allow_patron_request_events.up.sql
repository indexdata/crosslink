
INSERT INTO ill_transaction (id, timestamp, ill_transaction_data) VALUES ('00000000-0000-0000-0000-000000000001', now(), '{}');
INSERT INTO patron_request (id, state, side) VALUES ('00000000-0000-0000-0000-000000000002', 'NEW', 'borrowing');

ALTER TABLE event ADD COLUMN patron_request_id  VARCHAR NOT NULL DEFAULT '00000000-0000-0000-0000-000000000002',
                ADD FOREIGN KEY (patron_request_id) REFERENCES patron_request(id);

INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('invoke-action', 'TASK', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('patron-request-message', 'NOTICE', 1);