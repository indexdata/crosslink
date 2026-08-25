CREATE INDEX IF NOT EXISTS idx_pr_duplicate_requester_patron_created_at
    ON patron_request (requester_symbol, patron, created_at)
    INCLUDE (id);

CREATE INDEX IF NOT EXISTS idx_pr_supplier_unique_record_id
    ON patron_request ((ill_request->'bibliographicInfo'->>'supplierUniqueRecordId'));

CREATE INDEX IF NOT EXISTS idx_pr_title_lower
    ON patron_request (lower(ill_request->'bibliographicInfo'->>'title'));
