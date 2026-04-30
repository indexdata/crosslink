CREATE OR REPLACE FUNCTION norm_isxn(isxn text)
RETURNS text
LANGUAGE sql
IMMUTABLE STRICT
AS $$
SELECT upper(regexp_replace(isxn, '[^0-9Xx]', '', 'g'));
$$;

-- TODO: this should use norm_isxn, but the migration currently fails with
-- missing function when bibliographic_item_identifiers depends on it.
CREATE OR REPLACE FUNCTION bibliographic_item_identifiers(ill_request jsonb, item_code text)
RETURNS text[]
LANGUAGE sql
IMMUTABLE
AS $$
SELECT COALESCE(array_agg(identifier), ARRAY[]::text[])
FROM (
    SELECT upper(regexp_replace(item->>'bibliographicItemIdentifier', '[^0-9Xx]', '', 'g')) AS identifier
    FROM jsonb_array_elements(COALESCE(ill_request->'bibliographicInfo'->'bibliographicItemId', '[]'::jsonb)) AS item
    WHERE upper(item->'bibliographicItemIdentifierCode'->>'#text') = upper(item_code)
      AND item ? 'bibliographicItemIdentifier'
) identifiers
WHERE identifier <> '';
$$;

CREATE INDEX idx_pr_isbn
    ON patron_request
    USING gin (bibliographic_item_identifiers(ill_request, 'ISBN'));

CREATE INDEX idx_pr_issn
    ON patron_request
    USING gin (bibliographic_item_identifiers(ill_request, 'ISSN'));
