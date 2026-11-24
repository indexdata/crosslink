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
