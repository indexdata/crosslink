DROP VIEW patron_request_search_view ;

ALTER TABLE patron_request DROP COLUMN needs_attention;

DROP INDEX IF EXISTS idx_pr_state;
DROP INDEX IF EXISTS idx_pr_side;
DROP INDEX IF EXISTS idx_pr_requester_symbol;
DROP INDEX IF EXISTS idx_pr_supplier_symbol;
DROP INDEX IF EXISTS idx_pr_requester_req_id;
DROP INDEX IF EXISTS idx_pr_needs_attention;
DROP INDEX IF EXISTS idx_pr_timestamp;

DROP INDEX IF EXISTS idx_pr_service_type;
DROP INDEX IF EXISTS idx_pr_service_level;
DROP INDEX IF EXISTS idx_pr_needed_at;

DROP INDEX IF EXISTS idx_notification_pr_id;
DROP INDEX IF EXISTS idx_notification_pr_id_has_cost;
DROP INDEX IF EXISTS idx_notification_pr_id_unread;

DROP FUNCTION IF EXISTS immutable_to_timestamp;