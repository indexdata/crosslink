CREATE TABLE scheduled_task
(
    id          TEXT PRIMARY KEY,
    event_name  TEXT        NOT NULL,
    schedule    TEXT        NOT NULL,
    action_data JSONB,
    run_at      TIMESTAMPTZ,
    status      TEXT        NOT NULL DEFAULT 'pending',
    owner       TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ,
    FOREIGN KEY (event_name) REFERENCES event_config (event_name)
);

CREATE INDEX idx_scheduled_task_run_at ON scheduled_task (run_at) WHERE status = 'pending' AND run_at IS NOT NULL;

CREATE INDEX idx_scheduled_task_id_owner ON scheduled_task (id, owner);
CREATE INDEX idx_scheduled_task_owner ON scheduled_task (owner);