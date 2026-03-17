DROP VIEW patron_request_search_view ;

ALTER TABLE patron_request DROP COLUMN needs_attention;

DROP INDEX idx_pr_state;
DROP INDEX idx_pr_side;
DROP INDEX idx_pr_requester_symbol;
DROP INDEX idx_pr_supplier_symbol;
DROP INDEX idx_pr_requester_req_id;
DROP INDEX idx_pr_needs_attention;
DROP INDEX idx_pr_timestamp;

DROP INDEX idx_pr_service_type;
DROP INDEX idx_pr_service_level;
DROP INDEX idx_pr_needed_at;

DROP INDEX idx_notification_pr_id;
DROP INDEX idx_notification_pr_id_has_cost;
DROP INDEX idx_notification_pr_id_unread;

DROP OR REPLACE FUNCTION immutable_to_timestamp;