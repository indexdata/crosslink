DROP INDEX IF EXISTS idx_pr_issn;
DROP INDEX IF EXISTS idx_pr_isbn;
DROP FUNCTION IF EXISTS bibliographic_item_identifiers(jsonb, text);
DROP FUNCTION IF EXISTS norm_isxn(text);
