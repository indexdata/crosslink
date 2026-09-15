DO $$
DECLARE
  legacy_row record;
  base_name text;
  candidate_name text;
  suffix integer;
BEGIN
  FOR legacy_row IN
    SELECT id, consortium FROM tiers
    WHERE name IS NULL OR name !~ '[^[:space:]]'
    ORDER BY consortium, id
  LOOP
    base_name := 'Legacy tier ' || legacy_row.id::text;
    candidate_name := base_name;
    suffix := 0;
    WHILE EXISTS (
      SELECT 1 FROM tiers
      WHERE consortium = legacy_row.consortium
        AND id <> legacy_row.id
        AND name = candidate_name
    ) LOOP
      suffix := suffix + 1;
      candidate_name := base_name || ' (' || suffix::text || ')';
    END LOOP;
    UPDATE tiers SET name = candidate_name WHERE id = legacy_row.id;
  END LOOP;

  FOR legacy_row IN
    SELECT id, consortium FROM networks
    WHERE name IS NULL OR name !~ '[^[:space:]]'
    ORDER BY consortium, id
  LOOP
    base_name := 'Legacy network ' || legacy_row.id::text;
    candidate_name := base_name;
    suffix := 0;
    WHILE EXISTS (
      SELECT 1 FROM networks
      WHERE consortium = legacy_row.consortium
        AND id <> legacy_row.id
        AND name = candidate_name
    ) LOOP
      suffix := suffix + 1;
      candidate_name := base_name || ' (' || suffix::text || ')';
    END LOOP;
    UPDATE networks SET name = candidate_name WHERE id = legacy_row.id;
  END LOOP;
END
$$;

DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM tiers GROUP BY consortium, name HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'cannot add tier business key: duplicate consortium/name pairs exist';
  END IF;
  IF EXISTS (
    SELECT 1 FROM networks GROUP BY consortium, name HAVING count(*) > 1
  ) THEN
    RAISE EXCEPTION 'cannot add network business key: duplicate consortium/name pairs exist';
  END IF;
END
$$;

ALTER TABLE tiers ALTER COLUMN name SET NOT NULL;
ALTER TABLE networks ALTER COLUMN name SET NOT NULL;

ALTER TABLE tiers
  ADD CONSTRAINT tiers_consortium_name_unique UNIQUE (consortium, name);

ALTER TABLE networks
  ADD CONSTRAINT networks_consortium_name_unique UNIQUE (consortium, name);

ALTER TABLE tiers
  ADD CONSTRAINT tiers_name_not_blank CHECK (name ~ '[^[:space:]]');

ALTER TABLE networks
  ADD CONSTRAINT networks_name_not_blank CHECK (name ~ '[^[:space:]]');
