ALTER TABLE event
    DROP CONSTRAINT event_ill_transaction_id_fkey,
    DROP CONSTRAINT event_patron_request_id_fkey,
    ADD CONSTRAINT event_ill_transaction_id_fkey
        FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id) ON DELETE CASCADE,
    ADD CONSTRAINT event_patron_request_id_fkey
        FOREIGN KEY (patron_request_id) REFERENCES patron_request (id) ON DELETE CASCADE;

ALTER TABLE located_supplier
    DROP CONSTRAINT located_supplier_ill_transaction_id_fkey,
    ADD CONSTRAINT located_supplier_ill_transaction_id_fkey
        FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id) ON DELETE CASCADE;

ALTER TABLE item
    DROP CONSTRAINT item_pr_id_fkey,
    ADD CONSTRAINT item_pr_id_fkey
        FOREIGN KEY (pr_id) REFERENCES patron_request (id) ON DELETE CASCADE;

ALTER TABLE notification
    DROP CONSTRAINT notification_pr_id_fkey,
    ADD CONSTRAINT notification_pr_id_fkey
        FOREIGN KEY (pr_id) REFERENCES patron_request (id) ON DELETE CASCADE;

CREATE OR REPLACE FUNCTION prevent_synthetic_parent_delete()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'synthetic parent % cannot be deleted', OLD.id
        USING ERRCODE = 'integrity_constraint_violation';
END;
$$;

CREATE TRIGGER prevent_synthetic_ill_transaction_delete
    BEFORE DELETE ON ill_transaction
    FOR EACH ROW
    WHEN (OLD.id = '00000000-0000-0000-0000-000000000001')
    EXECUTE FUNCTION prevent_synthetic_parent_delete();

CREATE TRIGGER prevent_synthetic_patron_request_delete
    BEFORE DELETE ON patron_request
    FOR EACH ROW
    WHEN (OLD.id = '00000000-0000-0000-0000-000000000002')
    EXECUTE FUNCTION prevent_synthetic_parent_delete();
