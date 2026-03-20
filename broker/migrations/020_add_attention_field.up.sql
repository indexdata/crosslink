ALTER TABLE patron_request ADD COLUMN needs_attention BOOLEAN NOT NULL DEFAULT false;

CREATE OR REPLACE FUNCTION immutable_to_timestamp(text)
RETURNS timestamp
LANGUAGE sql
IMMUTABLE STRICT
AS $$
SELECT $1::timestamp;
$$;

CREATE OR REPLACE VIEW patron_request_search_view AS
SELECT
    pr.*,
    EXISTS (
        SELECT 1
        FROM notification n
        WHERE n.pr_id = pr.id
    ) AS has_notification,
    EXISTS (
        SELECT 1
        FROM notification n
        WHERE n.pr_id = pr.id and cost is not null
    ) AS has_cost,
    EXISTS (
        SELECT 1
        FROM notification n
        WHERE n.pr_id = pr.id and acknowledged_at is null
    ) AS has_unread_notification,
    pr.ill_request -> 'serviceInfo' ->> 'serviceType' AS service_type,
    pr.ill_request -> 'serviceInfo' -> 'serviceLevel' ->> '#text' AS service_level,
    immutable_to_timestamp(pr.ill_request -> 'serviceInfo' ->> 'needBeforeDate') AS needed_at
FROM patron_request pr;

CREATE INDEX idx_pr_state ON patron_request (state);
CREATE INDEX idx_pr_side ON patron_request (side);
CREATE INDEX idx_pr_requester_symbol ON patron_request (requester_symbol);
CREATE INDEX idx_pr_supplier_symbol ON patron_request (supplier_symbol);
CREATE INDEX idx_pr_requester_req_id ON patron_request (requester_req_id);
CREATE INDEX idx_pr_needs_attention ON patron_request (needs_attention);
CREATE INDEX idx_pr_timestamp ON patron_request (timestamp);

CREATE INDEX idx_pr_service_type ON patron_request ((ill_request -> 'serviceInfo' ->> 'serviceType'));
CREATE INDEX idx_pr_service_level ON patron_request ((ill_request -> 'serviceInfo' -> 'serviceLevel' ->> '#text'));
CREATE INDEX idx_pr_needed_at ON patron_request (immutable_to_timestamp(ill_request -> 'serviceInfo' ->> 'needBeforeDate'));

CREATE INDEX idx_notification_pr_id ON notification (pr_id);
CREATE INDEX idx_notification_pr_id_has_cost ON notification (pr_id) WHERE cost IS NOT NULL;
CREATE INDEX idx_notification_pr_id_unread ON notification (pr_id) WHERE acknowledged_at IS NULL;


