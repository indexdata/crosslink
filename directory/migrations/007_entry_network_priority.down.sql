DO $$
BEGIN
  IF EXISTS (
    SELECT network FROM entry_networks
    GROUP BY network HAVING count(DISTINCT priority) > 1
  ) THEN
    RAISE EXCEPTION 'cannot restore networks.priority: memberships have different priorities';
  END IF;
END $$;

ALTER TABLE networks ADD COLUMN priority integer NOT NULL DEFAULT 0;

UPDATE networks n
SET priority = en.priority
FROM entry_networks en
WHERE en.network = n.id;

ALTER TABLE entry_networks DROP COLUMN priority;
