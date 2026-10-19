CREATE INDEX idx_item_barcode_pr_id
    ON item (barcode varchar_pattern_ops, pr_id);

CREATE INDEX idx_item_item_id_pr_id
    ON item (item_id varchar_pattern_ops, pr_id)
    WHERE item_id IS NOT NULL;

CREATE INDEX idx_item_call_number_pr_id
    ON item (call_number varchar_pattern_ops, pr_id)
    WHERE call_number IS NOT NULL;
