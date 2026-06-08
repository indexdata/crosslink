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
    action_params     JSONB,
    FOREIGN KEY (scheduled_task_id) REFERENCES scheduled_task (id)
);

CREATE INDEX idx_batch_action_owner ON batch_action (owner);

DROP INDEX IF EXISTS idx_scheduled_task_id_owner;
DROP INDEX IF EXISTS idx_scheduled_task_owner;

ALTER TABLE scheduled_task
    DROP COLUMN owner;

ALTER TABLE scheduled_task
    RENAME COLUMN action_data TO payload;
