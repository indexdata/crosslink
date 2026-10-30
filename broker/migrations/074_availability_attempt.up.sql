-- Coordination state is separate from supplier rows so protocol updates cannot
-- restore an obsolete evaluation token. It is not exported/imported request data.
CREATE TABLE availability_attempt (
    ill_transaction_id VARCHAR PRIMARY KEY REFERENCES ill_transaction(id) ON DELETE CASCADE,
    token VARCHAR NOT NULL,
    located_supplier_id VARCHAR NOT NULL REFERENCES located_supplier(id) ON DELETE CASCADE
);

CREATE INDEX availability_attempt_supplier_idx ON availability_attempt(located_supplier_id);
