-- Keep existing audit records and their referenced event configuration on downgrade.
DELETE FROM event_config
WHERE event_name IN ('supplier-added', 'supplier-moved')
  AND NOT EXISTS (SELECT 1 FROM event WHERE event.event_name = event_config.event_name);
