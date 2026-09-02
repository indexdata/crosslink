ALTER TABLE item
    DROP COLUMN requester_lms_item_created;

-- Remove the dropped field from the denormalized patron_request.items cache.
UPDATE item SET barcode = barcode;
