ALTER TABLE batch_action_run
    ADD COLUMN plan_saved BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN overdue_cutoff TIMESTAMPTZ,
    ADD COLUMN overdue_cursor VARCHAR NOT NULL DEFAULT '',
    ADD COLUMN overdue_plan_size BIGINT NOT NULL DEFAULT 0;
ALTER TABLE batch_action_run_event ADD COLUMN work_started_at TIMESTAMPTZ;

-- Workers from before this migration could enter their handlers without a checkpoint.
UPDATE batch_action_run_event AS link
SET work_started_at = now()
FROM event
WHERE event.id = link.event_id AND event.event_status = 'PROCESSING';

CREATE TABLE batch_action_plan (
    run_id VARCHAR NOT NULL REFERENCES batch_action_run (id) ON DELETE CASCADE,
    position BIGINT NOT NULL,
    request_id VARCHAR NOT NULL,
    action_name VARCHAR,
    selection_error VARCHAR,
    PRIMARY KEY (run_id, position)
);
