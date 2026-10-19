CREATE INDEX idx_item_barcode_pr_id
    ON item (barcode, pr_id);

CREATE INDEX idx_item_item_id_pr_id
    ON item (item_id, pr_id)
    WHERE item_id IS NOT NULL;

CREATE INDEX idx_item_call_number_pr_id
    ON item (call_number, pr_id)
    WHERE call_number IS NOT NULL;
