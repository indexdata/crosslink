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

CREATE INDEX idx_scheduled_task_run_at ON scheduled_task (run_at) WHERE status = 'pending';
