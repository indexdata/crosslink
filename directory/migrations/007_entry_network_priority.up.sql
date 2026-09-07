ALTER TABLE entry_networks ADD COLUMN priority integer NOT NULL DEFAULT 0;

UPDATE entry_networks en
SET priority = n.priority
FROM networks n
WHERE n.id = en.network;

ALTER TABLE networks DROP COLUMN priority;
