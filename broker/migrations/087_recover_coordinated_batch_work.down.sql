DROP INDEX batch_action_run_event_recovery_idx;
ALTER TABLE batch_action_run_event
    DROP COLUMN recovery_reason,
    DROP COLUMN recovery_blocked_at,
    DROP COLUMN attempt_lock_id;
