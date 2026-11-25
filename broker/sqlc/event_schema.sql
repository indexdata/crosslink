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
