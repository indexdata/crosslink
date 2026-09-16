ALTER TABLE patron_request ADD COLUMN due_at TIMESTAMPTZ;
ALTER TABLE item ADD COLUMN lms_status VARCHAR NOT NULL DEFAULT 'UNKNOWN' CHECK (lms_status IN ('UNKNOWN', 'REQUESTED', 'ACCEPTED', 'CHECKED_OUT', 'CHECKED_IN', 'DELETED'));
ALTER TABLE item ADD COLUMN lms_due_date TIMESTAMPTZ;
UPDATE patron_request SET state = 'RECEIVED' WHERE side = 'borrowing' AND state IN ('CHECKED_OUT', 'CHECKED_IN');
UPDATE item SET lms_status = lms_status;
CREATE INDEX patron_request_overdue_idx ON patron_request (due_at) WHERE side = 'lending';
-- A broker-wide task covers both existing and future tenants. Owner-scoped
-- overdue tasks can also be configured through the normal scheduling API.
INSERT INTO scheduled_task (id, event_name, schedule, action_data, title, run_at, owner)
VALUES ('loan-overdue', 'invoke-batch-action', 'FREQ=MINUTELY;INTERVAL=15',
    '{"batchActionData":{"actionName":"overdue","selector":"side = lending and (state = RECEIVED or state = RENEWED)","taskId":"loan-overdue","owner":""}}',
    'Overdue loans', now() + interval '15 minutes', '');
DROP VIEW patron_request_search_view;
CREATE VIEW patron_request_search_view AS
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
    (unread.unread_notifications_count > 0) AS has_unread_notification,
    (pr.internal_note IS NOT NULL AND btrim(pr.internal_note) <> '') AS has_internal_note,
    pr.ill_request -> 'serviceInfo' ->> 'serviceType' AS service_type,
    pr.ill_request -> 'serviceInfo' -> 'serviceLevel' ->> '#text' AS service_level,
    immutable_to_timestamp(pr.ill_request -> 'serviceInfo' ->> 'needBeforeDate') AS needed_at,
    unread.unread_notifications_count AS unread_notifications_count,
    req_peer.name AS requester_name,
    sup_peer.name AS supplier_name
FROM patron_request pr
LEFT JOIN LATERAL (
    SELECT COUNT(*) AS unread_notifications_count
    FROM notification n
    WHERE n.pr_id = pr.id and n.acknowledged_at is null
) unread ON true
LEFT JOIN symbol req_sym ON req_sym.symbol_value = pr.requester_symbol
LEFT JOIN peer req_peer ON req_peer.id = req_sym.peer_id
LEFT JOIN symbol sup_sym ON sup_sym.symbol_value = pr.supplier_symbol
LEFT JOIN peer sup_peer ON sup_peer.id = sup_sym.peer_id;
