CREATE TABLE event_config
(
    event_name  VARCHAR PRIMARY KEY,
    event_type  VARCHAR NOT NULL,
    retry_count INT     NOT NULL DEFAULT 0
);

CREATE TABLE event
(
    id                 VARCHAR PRIMARY KEY,
    timestamp          TIMESTAMP NOT NULL,
    ill_transaction_id VARCHAR   NOT NULL,
    parent_id          VARCHAR,
    event_type         VARCHAR   NOT NULL,
    event_name         VARCHAR   NOT NULL,
    event_status       VARCHAR   NOT NULL,
    event_data         jsonb,
    result_data        jsonb,
    last_signal        VARCHAR   NOT NULL,
    patron_request_id  VARCHAR   NOT NULL DEFAULT '00000000-0000-0000-0000-000000000002',
    FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id) ON DELETE CASCADE,
    FOREIGN KEY (patron_request_id) REFERENCES patron_request (id) ON DELETE CASCADE,
    FOREIGN KEY (event_name) REFERENCES event_config (event_name)
);

CREATE TABLE batch_action_run (
    id VARCHAR PRIMARY KEY REFERENCES event (id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
    scheduled_task_id VARCHAR NOT NULL,
    owner VARCHAR NOT NULL,
    action_name VARCHAR NOT NULL,
    completed_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX batch_action_run_active_owner_action_idx
    ON batch_action_run (owner, action_name) WHERE completed_at IS NULL;

CREATE INDEX batch_action_run_active_task_idx
    ON batch_action_run (scheduled_task_id) WHERE completed_at IS NULL;

CREATE TABLE batch_action_run_event (
    event_id VARCHAR PRIMARY KEY REFERENCES event (id) ON DELETE CASCADE,
    run_id VARCHAR NOT NULL REFERENCES batch_action_run (id) ON DELETE CASCADE
);

CREATE INDEX batch_action_run_event_run_idx ON batch_action_run_event (run_id);

CREATE FUNCTION finish_batch_action_run_if_idle(p_run_id VARCHAR) RETURNS void
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM 1 FROM batch_action_run
    WHERE id = p_run_id AND completed_at IS NULL
    FOR UPDATE;
    IF FOUND THEN
        -- Read again after taking the lock shared with child creation and completion.
        UPDATE batch_action_run SET completed_at = now()
        WHERE id = p_run_id AND NOT EXISTS (
            SELECT 1 FROM batch_action_run_event
            JOIN event ON event.id = batch_action_run_event.event_id
            WHERE run_id = p_run_id AND event_status NOT IN ('SUCCESS', 'ERROR', 'PROBLEM')
        );
    END IF;
END;
$$;

CREATE FUNCTION finish_batch_action_runs_after_delete() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    v_run_id VARCHAR;
BEGIN
    FOR v_run_id IN SELECT DISTINCT run_id FROM deleted_links ORDER BY run_id LOOP
        PERFORM finish_batch_action_run_if_idle(v_run_id);
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE TRIGGER finish_batch_action_runs_after_delete
AFTER DELETE ON batch_action_run_event
REFERENCING OLD TABLE AS deleted_links
FOR EACH STATEMENT EXECUTE FUNCTION finish_batch_action_runs_after_delete();

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

CREATE TABLE batch_action_email_delivery (
    run_id VARCHAR PRIMARY KEY REFERENCES batch_action_run (id) ON DELETE CASCADE,
    selected_request_ids VARCHAR[] NOT NULL,
    queue_printed_actions BOOLEAN NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at TIMESTAMPTZ
);

ALTER TABLE batch_action_run
    ADD COLUMN plan_saved BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN overdue_cutoff TIMESTAMPTZ,
    ADD COLUMN overdue_cursor VARCHAR NOT NULL DEFAULT '',
    ADD COLUMN overdue_plan_size BIGINT NOT NULL DEFAULT 0;
ALTER TABLE batch_action_run_event ADD COLUMN work_started_at TIMESTAMPTZ;

CREATE TABLE batch_action_plan (
    run_id VARCHAR NOT NULL REFERENCES batch_action_run (id) ON DELETE CASCADE,
    position BIGINT NOT NULL,
    request_id VARCHAR NOT NULL,
    action_name VARCHAR,
    selection_error VARCHAR,
    PRIMARY KEY (run_id, position)
);
