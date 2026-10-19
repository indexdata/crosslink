DROP TRIGGER IF EXISTS trg_check_template_owner_labels_unique ON template;
DROP FUNCTION IF EXISTS check_template_owner_labels_unique();
DROP INDEX IF EXISTS idx_scheduled_task_owner_title;
DROP INDEX IF EXISTS idx_patron_request_lending_routing_identity;
ALTER TABLE scheduled_task
    DROP CONSTRAINT IF EXISTS chk_scheduled_task_batch_action_title;
