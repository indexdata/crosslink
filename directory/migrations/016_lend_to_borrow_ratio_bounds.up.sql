ALTER TABLE entries
    ADD CONSTRAINT entries_lend_to_borrow_ratio_bounds_check CHECK (
        lend_to_borrow_ratio IS NULL OR (
            length(lend_to_borrow_ratio) <= 15 AND
            lend_to_borrow_ratio ~ '^((0{0,3}[1-9]|0{0,2}[1-9][0-9]|0?[1-9][0-9]{2}|[1-9][0-9]{3})(\.[0-9]{1,2})?|0{1,4}\.([1-9][0-9]?|0[1-9])):((0{0,3}[1-9]|0{0,2}[1-9][0-9]|0?[1-9][0-9]{2}|[1-9][0-9]{3})(\.[0-9]{1,2})?|0{1,4}\.([1-9][0-9]?|0[1-9]))$'
        )
    );
