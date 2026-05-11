-- Index for looking up ILL transactions by requester (peer)
CREATE INDEX IF NOT EXISTS idx_ill_transaction_requester_id
    ON ill_transaction (requester_id);

-- Index for looking up items by patron request
CREATE INDEX IF NOT EXISTS idx_item_pr_id
    ON item (pr_id);

-- Index for looking up located suppliers by peer (supplier)
CREATE INDEX IF NOT EXISTS idx_located_supplier_supplier_id
    ON located_supplier (supplier_id);
