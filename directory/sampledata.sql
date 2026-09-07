-- Two institutions share one network, with independently assigned priorities.
WITH consortium AS (
  INSERT INTO entries (name, type)
  VALUES ('Example Consortium', 'consortium')
  RETURNING id
), institutions AS (
  INSERT INTO entries (name, type, parent)
  SELECT name, 'institution', consortium.id
  FROM consortium CROSS JOIN (VALUES ('First Library'), ('Second Library')) AS names(name)
  RETURNING id, name
), network AS (
  INSERT INTO networks (name, consortium)
  SELECT 'Shared Network', id FROM consortium
  RETURNING id
)
INSERT INTO entry_networks (entry, network, priority)
SELECT institutions.id, network.id,
       CASE institutions.name WHEN 'First Library' THEN 1 ELSE 5 END
FROM institutions CROSS JOIN network;
