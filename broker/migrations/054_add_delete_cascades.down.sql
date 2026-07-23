DROP TRIGGER prevent_synthetic_patron_request_delete ON patron_request;
DROP TRIGGER prevent_synthetic_ill_transaction_delete ON ill_transaction;
DROP FUNCTION prevent_synthetic_parent_delete();

ALTER TABLE event
    DROP CONSTRAINT event_ill_transaction_id_fkey,
    DROP CONSTRAINT event_patron_request_id_fkey,
    ADD CONSTRAINT event_ill_transaction_id_fkey
        FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id),
    ADD CONSTRAINT event_patron_request_id_fkey
        FOREIGN KEY (patron_request_id) REFERENCES patron_request (id);

ALTER TABLE located_supplier
    DROP CONSTRAINT located_supplier_ill_transaction_id_fkey,
    ADD CONSTRAINT located_supplier_ill_transaction_id_fkey
        FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id);

ALTER TABLE item
    DROP CONSTRAINT item_pr_id_fkey,
    ADD CONSTRAINT item_pr_id_fkey
        FOREIGN KEY (pr_id) REFERENCES patron_request (id);

ALTER TABLE notification
    DROP CONSTRAINT notification_pr_id_fkey,
    ADD CONSTRAINT notification_pr_id_fkey
        FOREIGN KEY (pr_id) REFERENCES patron_request (id);
