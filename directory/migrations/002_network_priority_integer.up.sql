DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM networks
    WHERE priority::text IN ('NaN', 'Infinity', '-Infinity')
       OR priority < -2147483648
       OR priority > 2147483647
       OR priority <> trunc(priority)
  ) THEN
    RAISE EXCEPTION 'cannot migrate networks.priority to integer: non-integral or out-of-range values exist';
  END IF;
END
$$;

ALTER TABLE networks
  ALTER COLUMN priority DROP DEFAULT,
  ALTER COLUMN priority TYPE integer USING priority::integer,
  ALTER COLUMN priority SET DEFAULT 0;
