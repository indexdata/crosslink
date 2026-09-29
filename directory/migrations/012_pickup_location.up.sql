ALTER TABLE ill_configs ADD COLUMN is_pickup_location BOOLEAN;

INSERT INTO ill_configs (entry, is_pickup_location)
SELECT entry, TRUE FROM lms_configs WHERE requester_pickup_location IS NOT NULL
ON CONFLICT (entry) DO UPDATE SET is_pickup_location = TRUE;
