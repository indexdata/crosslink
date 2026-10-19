INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('supplier-added', 'NOTICE', 0), ('supplier-moved', 'NOTICE', 0)
ON CONFLICT (event_name) DO NOTHING;
