DROP TABLE batch_action_plan;
ALTER TABLE batch_action_run_event DROP COLUMN work_started_at;
ALTER TABLE batch_action_run
    DROP COLUMN overdue_plan_size,
    DROP COLUMN overdue_cursor,
    DROP COLUMN overdue_cutoff,
    DROP COLUMN plan_saved;
