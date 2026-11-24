ALTER TABLE batch_action_run_event
    ADD COLUMN attempt_lock_id VARCHAR,
    ADD COLUMN recovery_blocked_at TIMESTAMPTZ,
    ADD COLUMN recovery_reason VARCHAR;

CREATE INDEX batch_action_run_event_recovery_idx
    ON batch_action_run_event (event_id) WHERE recovery_blocked_at IS NULL;
