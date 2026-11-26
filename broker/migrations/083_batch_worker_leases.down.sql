UPDATE event
SET last_signal = '',
    event_status = CASE WHEN event_status IN ('NEW', 'PROCESSING') THEN 'ERROR' ELSE event_status END,
    result_data = CASE WHEN event_status IN ('NEW', 'PROCESSING')
        THEN '{"eventError":{"Message":"task interrupted during broker rollback","Cause":"The broker stopped before this task completed."}}'::jsonb
        ELSE result_data END
WHERE ((event_status IN ('NEW', 'PROCESSING') OR last_signal <> '')
    AND id IN (SELECT event_id FROM batch_action_run_event))
   OR (event_type = 'TASK' AND event_status IN ('NEW', 'PROCESSING') AND (
        (event_name IN ('invoke-batch-action', 'invoke-background-action') AND event_data -> 'batchActionData' IS NOT NULL)
        OR event_name IN ('confirm-requester-msg', 'confirm-supplier-msg')));

DROP TRIGGER track_message_confirmation_expiry ON event;
DROP FUNCTION track_message_confirmation_expiry();
DROP TABLE message_confirmation_expiry;

DROP FUNCTION lock_batch_worker(VARCHAR);
DROP TABLE batch_action_run_event;
DROP TABLE batch_action_run;
