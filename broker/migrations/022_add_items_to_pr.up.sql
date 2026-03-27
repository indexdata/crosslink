ALTER TABLE patron_request ADD COLUMN items JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE OR REPLACE FUNCTION update_patron_request_items()
RETURNS TRIGGER AS $$
BEGIN
UPDATE patron_request
SET items = (
    SELECT jsonb_agg(
                   (to_jsonb(i) - 'pr_id') ||
                   jsonb_build_object(
                           'created_at',
                           to_char(i.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US') || to_char(i.created_at, 'TZH:TZM')
                   )
           )
    FROM item i
    WHERE i.pr_id = NEW.pr_id
)
WHERE id = NEW.pr_id;

RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create the trigger
CREATE TRIGGER trigger_update_patron_request_items
    AFTER INSERT OR UPDATE ON item
                        FOR EACH ROW
                        EXECUTE FUNCTION update_patron_request_items();


-- Add the search field as a tsvector column
ALTER TABLE patron_request
    ADD COLUMN search tsvector,
    ADD COLUMN language regconfig NOT NULL DEFAULT 'english';

-- Create a trigger function to update the search tsvector
CREATE OR REPLACE FUNCTION update_patron_request_search_tsvector()
RETURNS TRIGGER AS $$
BEGIN
    -- Update the search tsvector column
    NEW.search := to_tsvector(NEW.language,
        COALESCE(NEW.requester_req_id, '') || ' ' ||
        COALESCE(NEW.patron, '') || ' ' ||
        COALESCE(NEW.ill_request->'patronInfo'->>'givenName', '') || ' ' ||
        COALESCE(NEW.ill_request->'patronInfo'->>'surname', '') || ' ' ||
        COALESCE(NEW.ill_request->'patronInfo'->>'patronId', '') || ' ' ||
        COALESCE(NEW.ill_request->'bibliographicInfo'->>'title', '') || ' ' ||
        COALESCE(NEW.ill_request->'bibliographicInfo'->>'author', '') || ' ' ||
        COALESCE(
            (SELECT string_agg(
                COALESCE(item->>'item_id', '') || ' ' ||
                COALESCE(item->>'barcode', '') || ' ' ||
                COALESCE(item->>'call_number', ''), ' '
            )
            FROM jsonb_array_elements(NEW.items) AS item), ''
        )
    );

RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create a trigger to update the search tsvector on insert or update
CREATE TRIGGER trigger_update_patron_request_search_tsvector
    BEFORE INSERT OR UPDATE ON patron_request
                         FOR EACH ROW
                         EXECUTE FUNCTION update_patron_request_search_tsvector();

CREATE INDEX idx_patron_request_search ON patron_request USING gin(search);

DROP VIEW IF EXISTS patron_request_search_view;
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

-- One-time backfill of items for existing patron_request rows
UPDATE patron_request pr
SET items = COALESCE(
        (
            SELECT jsonb_agg(
                           (to_jsonb(i) - 'pr_id') ||
                           jsonb_build_object(
                                   'created_at',
                                   to_char(i.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US') || to_char(i.created_at, 'TZH:TZM')
                           )
                   )
            FROM item i
            WHERE i.pr_id = pr.id
        ),
        '[]'::jsonb
            );
-- One-time backfill of search tsvector for existing patron_request rows
UPDATE patron_request pr
SET search = to_tsvector(
        pr.language,
        COALESCE(pr.requester_req_id, '') || ' ' ||
        COALESCE(pr.patron, '') || ' ' ||
        COALESCE(pr.ill_request->'patronInfo'->>'givenName', '') || ' ' ||
        COALESCE(pr.ill_request->'patronInfo'->>'surname', '') || ' ' ||
        COALESCE(pr.ill_request->'patronInfo'->>'patronId', '') || ' ' ||
        COALESCE(pr.ill_request->'bibliographicInfo'->>'title', '') || ' ' ||
        COALESCE(pr.ill_request->'bibliographicInfo'->>'author', '') || ' ' ||
        COALESCE(
                (
                    SELECT string_agg(
                                   COALESCE(item->>'item_id', '') || ' ' ||
                                   COALESCE(item->>'barcode', '') || ' ' ||
                                   COALESCE(item->>'call_number', ''),
                                   ' '
                           )
                    FROM jsonb_array_elements(pr.items) AS item
                ),
                ''
        )
             );