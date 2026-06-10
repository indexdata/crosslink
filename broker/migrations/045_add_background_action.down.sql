DELETE FROM event WHERE event_name = 'invoke-background-action';
DELETE FROM event_config WHERE event_name = 'invoke-background-action';

ALTER TABLE scheduled_task
    DROP COLUMN title;
