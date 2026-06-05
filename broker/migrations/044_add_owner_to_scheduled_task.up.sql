ALTER TABLE scheduled_task
    ADD COLUMN owner TEXT NOT NULL DEFAULT '';

UPDATE scheduled_task st
SET owner = ba.owner
FROM batch_action ba
WHERE ba.scheduled_task_id = st.id
  AND st.owner = '';

DROP TABLE batch_action;

