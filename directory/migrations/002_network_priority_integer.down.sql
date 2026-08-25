ALTER TABLE networks
  ALTER COLUMN priority DROP DEFAULT,
  ALTER COLUMN priority TYPE double precision USING priority::double precision,
  ALTER COLUMN priority SET DEFAULT 0.0;
