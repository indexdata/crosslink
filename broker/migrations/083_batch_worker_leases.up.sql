CREATE TABLE batch_action_run (
    id VARCHAR PRIMARY KEY REFERENCES event (id) ON DELETE CASCADE,
    scheduled_task_id VARCHAR NOT NULL,
    owner VARCHAR NOT NULL,
    action_name VARCHAR NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE INDEX batch_action_run_active_action_idx
    ON batch_action_run (action_name, owner) WHERE completed_at IS NULL;
CREATE INDEX batch_action_run_active_task_idx
    ON batch_action_run (scheduled_task_id) WHERE completed_at IS NULL;

CREATE TABLE batch_action_run_event (
    event_id VARCHAR PRIMARY KEY REFERENCES event (id) ON DELETE CASCADE,
    run_id VARCHAR NOT NULL REFERENCES batch_action_run (id) ON DELETE CASCADE,
    -- Asynchronous tasks own their lease. Inline children share their worker's lease.
    worker_event_id VARCHAR NOT NULL,
    ready_at TIMESTAMPTZ,
    lease_expires_at TIMESTAMPTZ,
    lease_cap_at TIMESTAMPTZ,
    finished BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX batch_action_run_event_run_idx ON batch_action_run_event (run_id);
CREATE INDEX batch_action_run_event_unfinished_idx
    ON batch_action_run_event (run_id) WHERE NOT finished;
CREATE INDEX batch_action_run_event_worker_idx
    ON batch_action_run_event (worker_event_id) WHERE NOT finished;
CREATE INDEX batch_action_run_event_ready_idx
    ON batch_action_run_event (ready_at, event_id) WHERE ready_at IS NOT NULL;
CREATE INDEX batch_action_run_event_leases_idx
    ON batch_action_run_event (lease_expires_at, event_id) WHERE lease_expires_at IS NOT NULL;

-- A short write transaction holds this row lock through commit. Recovery uses
-- the same lock and rechecks expiry once it acquires it.
CREATE FUNCTION lock_batch_worker(p_event_id VARCHAR) RETURNS BOOLEAN
LANGUAGE plpgsql AS $$
DECLARE
    expires TIMESTAMPTZ;
BEGIN
    SELECT lease_expires_at INTO expires FROM batch_action_run_event
    WHERE event_id = p_event_id FOR UPDATE;
    RETURN COALESCE(expires > clock_timestamp(), FALSE);
END;
$$;

CREATE TABLE message_confirmation_expiry (
    event_id VARCHAR PRIMARY KEY REFERENCES event (id) ON DELETE CASCADE,
    -- A confirmation task is created after its request starts waiting.
    -- Leave more than the handler's 30-second maximum wait for any replica.
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (clock_timestamp() + INTERVAL '1 minute')
);

CREATE INDEX message_confirmation_expiry_due_idx
    ON message_confirmation_expiry (expires_at, event_id);

CREATE FUNCTION track_message_confirmation_expiry() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.event_status IN ('NEW', 'PROCESSING') THEN
        INSERT INTO message_confirmation_expiry (event_id)
        VALUES (NEW.id) ON CONFLICT DO NOTHING;
    ELSE
        DELETE FROM message_confirmation_expiry WHERE event_id = NEW.id;
    END IF;
    RETURN NULL;
END;
$$;

CREATE TRIGGER track_message_confirmation_expiry
AFTER INSERT OR UPDATE OF event_status ON event
FOR EACH ROW WHEN (
    NEW.event_type = 'TASK'
    AND NEW.event_name IN ('confirm-requester-msg', 'confirm-supplier-msg')
)
EXECUTE FUNCTION track_message_confirmation_expiry();

-- Older binaries do not record ownership. Apply this migration after draining
-- and stopping all old broker replicas. Legacy local-message tasks were not
-- linked to the batch or protocol task that invoked them.
WITH RECURSIVE inline_ancestors AS (
    SELECT id AS child_id, parent_id AS ancestor_id FROM event
    WHERE event_type = 'TASK' AND event_status IN ('NEW', 'PROCESSING')
        AND event_name = 'invoke-action' AND parent_id IS NOT NULL
    UNION
    SELECT a.child_id, parent.parent_id FROM inline_ancestors a
    JOIN event parent ON parent.id = a.ancestor_id
    WHERE parent.event_name = 'invoke-action' AND parent.parent_id IS NOT NULL
)
UPDATE event
SET event_status = 'ERROR', last_signal = '',
    result_data = '{"eventError":{"Message":"task interrupted during broker upgrade","Cause":"The previous broker stopped before this task completed."}}'
WHERE event_type = 'TASK' AND event_status IN ('NEW', 'PROCESSING')
    AND (
        event_name = 'patron-request-message'
        OR (event_name IN ('invoke-batch-action', 'invoke-background-action') AND event_data -> 'batchActionData' IS NOT NULL)
        OR id IN (
            SELECT a.child_id FROM inline_ancestors a
            JOIN event ancestor ON ancestor.id = a.ancestor_id
            WHERE ancestor.event_type = 'TASK'
                AND (ancestor.event_name = 'patron-request-message'
                     OR (ancestor.event_name = 'invoke-background-action'
                         AND ancestor.event_data -> 'batchActionData' IS NOT NULL))
        )
    );

-- Give unfinished legacy tasks the same grace period on upgrade.
INSERT INTO message_confirmation_expiry (event_id)
SELECT id FROM event
WHERE event_type = 'TASK'
    AND event_name IN ('confirm-requester-msg', 'confirm-supplier-msg')
    AND event_status IN ('NEW', 'PROCESSING')
ON CONFLICT DO NOTHING;
