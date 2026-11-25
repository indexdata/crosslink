DROP TRIGGER track_message_confirmation_expiry ON event;
DROP FUNCTION track_message_confirmation_expiry();
DROP TABLE message_confirmation_expiry;

DROP FUNCTION lock_batch_worker(VARCHAR);
DROP TABLE batch_action_run_event;
DROP TABLE batch_action_run;
