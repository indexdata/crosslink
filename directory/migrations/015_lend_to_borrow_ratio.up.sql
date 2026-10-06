ALTER TABLE entries
    ADD COLUMN lend_to_borrow_ratio text,
    ADD CONSTRAINT entries_lend_to_borrow_ratio_check CHECK (
        lend_to_borrow_ratio IS NULL OR
        lend_to_borrow_ratio ~ '^(0*[1-9][0-9]*(\.[0-9]+)?|0+\.[0-9]*[1-9][0-9]*):(0*[1-9][0-9]*(\.[0-9]+)?|0+\.[0-9]*[1-9][0-9]*)$'
    );
