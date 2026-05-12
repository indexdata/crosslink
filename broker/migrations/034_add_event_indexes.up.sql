CREATE INDEX idx_event_ill_transaction_timestamp
    ON event (ill_transaction_id, timestamp, id);

CREATE INDEX idx_event_patron_request_timestamp
    ON event (patron_request_id, timestamp, id);

CREATE INDEX idx_event_incomplete_by_domain_name_timestamp
    ON event (patron_request_id, ill_transaction_id, event_type, event_name, timestamp, id)
    WHERE event_status IN ('NEW', 'PROCESSING');
