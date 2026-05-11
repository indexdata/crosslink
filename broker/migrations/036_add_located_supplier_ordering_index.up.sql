-- Add index for efficient ordered supplier selection by ill_transaction and ordinal
CREATE INDEX IF NOT EXISTS idx_located_supplier_illtx_ordinal
  ON located_supplier (ill_transaction_id, ordinal);
