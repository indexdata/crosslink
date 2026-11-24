DROP TRIGGER reserve_batch_action_scope ON batch_action_run;
DROP FUNCTION reserve_batch_action_scope();
DROP TABLE batch_action_reservation_lock;
DROP INDEX batch_action_run_active_action_idx;

DROP FUNCTION lock_batch_action_attempt(VARCHAR, VARCHAR);
ALTER TABLE batch_action_run_event
    DROP COLUMN attempt_claimed_at,
    DROP COLUMN attempt_id;
