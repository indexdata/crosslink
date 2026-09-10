CREATE TABLE migration_007_network_priority_backup (
  network uuid PRIMARY KEY REFERENCES networks (id) ON DELETE CASCADE,
  priority integer NOT NULL
);

INSERT INTO migration_007_network_priority_backup (network, priority)
SELECT id, priority FROM networks;

ALTER TABLE entry_networks ADD COLUMN priority integer NOT NULL DEFAULT 0;

UPDATE entry_networks en
SET priority = n.priority
FROM networks n
WHERE n.id = en.network;

ALTER TABLE networks DROP COLUMN priority;
