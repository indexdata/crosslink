CREATE INDEX IF NOT EXISTS idx_ill_transaction_requester_timestamp
    ON ill_transaction (requester_symbol, timestamp DESC)
    INCLUDE (id);