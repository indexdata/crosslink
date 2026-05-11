-- Remove index for located_supplier ordering (rollback)
DROP INDEX IF EXISTS idx_located_supplier_illtx_ordinal;
