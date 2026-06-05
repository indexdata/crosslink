ALTER TABLE scheduled_task
    DROP COLUMN owner;

CREATE TABLE batch_action
(
    id                TEXT PRIMARY KEY,
    action_name       TEXT        NOT NULL,
    schedule          TEXT        NOT NULL,
    batch_query       TEXT        NOT NULL,
    owner             TEXT        NOT NULL,
    scheduled_task_id TEXT        NOT NULL,
    action_params     JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ,
    FOREIGN KEY (scheduled_task_id) REFERENCES scheduled_task (id)
);
CREATE INDEX idx_batch_action_owner ON batch_action (owner);

