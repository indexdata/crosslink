
ALTER TABLE scheduled_task
    RENAME COLUMN schedule TO cron_expr;