DROP VIEW IF EXISTS patron_request_search_view;

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
LEFT JOIN LATERAL (
    SELECT p.name
    FROM symbol s JOIN peer p ON p.id = s.peer_id
    WHERE s.symbol_value = pr.requester_symbol
) req_peer ON true
LEFT JOIN LATERAL (
    SELECT p.name
    FROM symbol s JOIN peer p ON p.id = s.peer_id
    WHERE s.symbol_value = pr.supplier_symbol
) sup_peer ON true;
