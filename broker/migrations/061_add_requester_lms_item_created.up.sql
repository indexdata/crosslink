ALTER TABLE item
    ADD COLUMN requester_lms_item_created BOOLEAN NOT NULL DEFAULT false;

-- Refresh the denormalized patron_request.items cache with the new field.
UPDATE item SET barcode = barcode;
