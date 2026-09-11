DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM tiers WHERE name IS NULL OR name !~ '[^[:space:]]') THEN
    RAISE EXCEPTION 'cannot add tier business key: tiers contain null or blank names';
  END IF;
  IF EXISTS (SELECT 1 FROM networks WHERE name IS NULL OR name !~ '[^[:space:]]') THEN
    RAISE EXCEPTION 'cannot add network business key: networks contain null or blank names';
  END IF;
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
