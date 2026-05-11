-- Enforce unique ordinal per ill_transaction and provide an efficient index for ordered supplier selection
ALTER TABLE located_supplier
    ADD CONSTRAINT uq_located_supplier_illtx_ordinal UNIQUE (ill_transaction_id, ordinal);
