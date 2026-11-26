UPDATE event
SET event_status = 'ERROR', last_signal = '',
    result_data = '{"eventError":{"Message":"task interrupted during broker rollback","Cause":"The broker stopped before this task completed."}}'
WHERE event_type = 'TASK' AND event_status IN ('NEW', 'PROCESSING')
    AND (
        id IN (SELECT event_id FROM batch_action_run_event)
        OR (event_name IN ('invoke-batch-action', 'invoke-background-action') AND event_data -> 'batchActionData' IS NOT NULL)
        OR event_name IN ('confirm-requester-msg', 'confirm-supplier-msg')
    );

DROP TRIGGER track_message_confirmation_expiry ON event;
DROP FUNCTION track_message_confirmation_expiry();
DROP TABLE message_confirmation_expiry;

DROP FUNCTION lock_batch_worker(VARCHAR);
DROP TABLE batch_action_run_event;
DROP TABLE batch_action_run;
