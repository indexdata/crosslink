ALTER TABLE entries
  ADD COLUMN lender_of_last_resort text[],
  ADD COLUMN duplicate_check_window_hours integer CHECK (duplicate_check_window_hours >= 0);

UPDATE entries e
SET
  lender_of_last_resort = i.lenders_of_last_resort,
  duplicate_check_window_hours = i.duplicate_check_window_hours
FROM ill_configs i
WHERE i.entry = e.id;

DROP TABLE ill_configs;
