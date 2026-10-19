CREATE TABLE ill_configs (
  id uuid DEFAULT gen_random_uuid() PRIMARY KEY,
  entry uuid NOT NULL UNIQUE REFERENCES entries (id) ON DELETE CASCADE,
  iso18626_url text,
  iso18626_vendor varchar(64),
  lenders_of_last_resort text[],
  include_requesting_agency_info boolean,
  include_supplier_info boolean,
  include_return_info boolean,
  include_vendor_note boolean,
  use_offered_costs boolean,
  note_field_separator text,
  supplier_patron_pattern text,
  duplicate_check_window_hours integer CHECK (duplicate_check_window_hours >= 0)
);

INSERT INTO ill_configs (
  entry,
  lenders_of_last_resort,
  duplicate_check_window_hours
)
SELECT
  id,
  lender_of_last_resort,
  duplicate_check_window_hours
FROM entries
WHERE lender_of_last_resort IS NOT NULL
   OR duplicate_check_window_hours IS NOT NULL;

ALTER TABLE entries
  DROP COLUMN lender_of_last_resort,
  DROP COLUMN duplicate_check_window_hours;
