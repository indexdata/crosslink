ALTER TABLE patron_request
    ADD COLUMN updated_at timestamp;

CREATE INDEX idx_patron_request_updated_at
    ON patron_request(updated_at);

DROP VIEW IF EXISTS patron_request_search_view;

ALTER TABLE patron_request
    RENAME COLUMN "timestamp" TO created_at;

CREATE INDEX idx_patron_request_title_tsv
    ON patron_request
    USING gin (
    to_tsvector(language, ill_request->'bibliographicInfo'->>'title')
    );

CREATE INDEX idx_patron_request_author_tsv
    ON patron_request
    USING gin (
    to_tsvector(language, ill_request->'bibliographicInfo'->>'author')
    );


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