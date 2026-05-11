-- Remove unique constraint on located_supplier ordinal (rollback)
ALTER TABLE located_supplier
    DROP CONSTRAINT IF EXISTS uq_located_supplier_illtx_ordinal;
