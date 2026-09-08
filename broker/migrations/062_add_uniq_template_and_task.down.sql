DROP TRIGGER IF EXISTS trg_check_template_owner_labels_unique ON template;
DROP FUNCTION IF EXISTS check_template_owner_labels_unique();
DROP INDEX IF EXISTS idx_scheduled_task_owner_title;
