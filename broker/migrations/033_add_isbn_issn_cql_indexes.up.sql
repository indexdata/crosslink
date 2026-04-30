CREATE OR REPLACE FUNCTION bibliographic_item_identifiers(ill_request jsonb, item_code text)
RETURNS text[]
LANGUAGE sql
IMMUTABLE
AS $$
SELECT COALESCE(array_agg(item->>'bibliographicItemIdentifier'), ARRAY[]::text[])
FROM jsonb_array_elements(COALESCE(ill_request->'bibliographicInfo'->'bibliographicItemId', '[]'::jsonb)) AS item
WHERE upper(item->'bibliographicItemIdentifierCode'->>'#text') = upper(item_code)
  AND item ? 'bibliographicItemIdentifier';
$$;

CREATE INDEX idx_pr_isbn
    ON patron_request
    USING gin (bibliographic_item_identifiers(ill_request, 'ISBN'));

CREATE INDEX idx_pr_issn
    ON patron_request
    USING gin (bibliographic_item_identifiers(ill_request, 'ISSN'));
