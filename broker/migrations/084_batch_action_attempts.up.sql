ALTER TABLE batch_action_run_event
    ADD COLUMN attempt_id VARCHAR,
    ADD COLUMN attempt_claimed_at TIMESTAMPTZ;

-- Attempt changes and fenced writes serialize on the event row. Read the
-- attempt after acquiring the lock so a stale snapshot cannot pass the fence.
CREATE FUNCTION lock_batch_action_attempt(p_event_id VARCHAR, p_attempt_id VARCHAR) RETURNS boolean
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM event WHERE id = p_event_id AND event_status = 'PROCESSING' FOR UPDATE;
    IF NOT FOUND THEN
        RETURN false;
    END IF;
    RETURN EXISTS (
        SELECT 1 FROM batch_action_run_event
        WHERE event_id = p_event_id AND attempt_id = p_attempt_id
    );
END;
$$;

CREATE INDEX batch_action_run_active_action_idx
    ON batch_action_run (action_name) WHERE completed_at IS NULL;

CREATE TABLE batch_action_reservation_lock (
    action_name VARCHAR PRIMARY KEY
);

CREATE FUNCTION reserve_batch_action_scope() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.completed_at IS NOT NULL THEN
        RETURN NEW;
    END IF;

    -- Serialize reservation inserts for this action through commit. Updating
    -- the row also forces a serialization error under snapshot isolation if a
    -- concurrent reservation committed after this transaction's snapshot.
    INSERT INTO batch_action_reservation_lock (action_name) VALUES (NEW.action_name)
    ON CONFLICT (action_name) DO UPDATE SET action_name = EXCLUDED.action_name;

    IF EXISTS (
        SELECT 1 FROM batch_action_run
        WHERE action_name = NEW.action_name AND completed_at IS NULL
            AND (NEW.owner = '' OR owner = '')
    ) THEN
        -- Skip the insert so the queue's RETURNING reports a reservation conflict.
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reserve_batch_action_scope
BEFORE INSERT ON batch_action_run
FOR EACH ROW EXECUTE FUNCTION reserve_batch_action_scope();
