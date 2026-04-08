
DROP INDEX IF EXISTS idx_patron_request_author_tsv;

DROP INDEX IF EXISTS idx_patron_request_title_tsv;

DROP INDEX IF EXISTS idx_patron_request_updated_at;

DROP VIEW IF EXISTS patron_request_search_view;

ALTER TABLE patron_request RENAME COLUMN created_at TO "timestamp";

ALTER TABLE patron_request DROP COLUMN IF EXISTS updated_at;

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
