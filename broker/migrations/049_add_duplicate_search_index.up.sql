CREATE INDEX IF NOT EXISTS idx_ill_transaction_requester_timestamp
    ON ill_transaction (requester_symbol, timestamp)
    INCLUDE (id);

CREATE INDEX IF NOT EXISTS idx_ill_transaction_isbn
    ON ill_transaction
    USING gin (bibliographic_item_identifiers(ill_transaction_data, 'ISBN'));

CREATE INDEX IF NOT EXISTS idx_ill_transaction_issn
    ON ill_transaction
    USING gin (bibliographic_item_identifiers(ill_transaction_data, 'ISSN'));

CREATE INDEX idx_ill_transaction_title_tsv
    ON ill_transaction USING GIN (to_tsvector('simple', ill_transaction_data->'bibliographicInfo'->>'title'));

CREATE INDEX IF NOT EXISTS idx_ill_transaction_supplier_unique_record_id
    ON ill_transaction ((ill_transaction_data->'bibliographicInfo'->>'supplierUniqueRecordId'));

CREATE INDEX IF NOT EXISTS idx_ill_transaction_patron_id
    ON ill_transaction ((ill_transaction_data->'patronInfo'->>'patronId'));

CREATE INDEX IF NOT EXISTS idx_ill_transaction_service_type
    ON ill_transaction ((ill_transaction_data->'serviceInfo'->>'serviceType'));