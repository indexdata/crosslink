ALTER TABLE scheduled_task
    ADD COLUMN owner TEXT NOT NULL DEFAULT '';

ALTER TABLE scheduled_task
    RENAME COLUMN payload TO action_data;

UPDATE scheduled_task st
SET owner = ba.owner
FROM batch_action ba
WHERE ba.scheduled_task_id = st.id
  AND st.owner = '';

ALTER TABLE scheduled_task
    ALTER COLUMN owner DROP DEFAULT;
CREATE INDEX IF NOT EXISTS idx_scheduled_task_id_owner ON scheduled_task (id, owner);
CREATE INDEX IF NOT EXISTS idx_scheduled_task_owner ON scheduled_task (owner);

DROP TABLE batch_action;

