ALTER TABLE patron_request
    ADD COLUMN last_action VARCHAR,
    ADD COLUMN last_action_outcome VARCHAR,
    ADD COLUMN last_action_result VARCHAR;
