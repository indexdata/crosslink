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