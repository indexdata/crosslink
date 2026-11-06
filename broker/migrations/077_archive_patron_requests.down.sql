DROP FUNCTION archive_lending_request(TEXT, TEXT, TEXT, TIMESTAMP, TIMESTAMP, BOOLEAN);
DROP FUNCTION archive_borrowing_request(TEXT, TEXT, TEXT, TIMESTAMP, TIMESTAMP, BOOLEAN);
DROP FUNCTION archive_anonymize_jsonb(JSONB, TEXT);
DROP FUNCTION archive_pick_scalars(JSONB, TEXT[]);
DROP TABLE archived_resource;
DROP INDEX idx_pr_lending_archive_cursor;
DROP INDEX idx_pr_borrowing_archive_cursor;
