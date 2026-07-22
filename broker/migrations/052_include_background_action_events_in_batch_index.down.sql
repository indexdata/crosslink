DROP INDEX idx_event_batch_action_task_timestamp;

CREATE INDEX idx_event_batch_action_task_timestamp
    ON event ((event_data -> 'batchActionData' ->> 'taskId'), timestamp DESC)
    WHERE event_name = 'invoke-batch-action';
