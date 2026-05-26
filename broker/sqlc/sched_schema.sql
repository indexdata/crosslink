CREATE TABLE scheduled_task
(
    id         TEXT PRIMARY KEY,
    event_name TEXT        NOT NULL,
    cron_expr  TEXT        NOT NULL,
    payload    JSONB,
    run_at     TIMESTAMPTZ,
    status     TEXT        NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ,
    FOREIGN KEY (event_name) REFERENCES event_config (event_name)
);

CREATE INDEX idx_scheduled_task_run_at ON scheduled_task (run_at) WHERE status = 'pending' AND run_at IS NOT NULL;

CREATE TABLE batch_action
(
    id                TEXT PRIMARY KEY,
    action_name       TEXT        NOT NULL,
    schedule          TEXT        NOT NULL,
    batch_query       TEXT        NOT NULL,
    owner             TEXT        NOT NULL,
    scheduled_task_id TEXT        NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ,
    FOREIGN KEY (scheduled_task_id) REFERENCES scheduled_task (id)
);
CREATE INDEX idx_batch_action_owner ON batch_action (owner);