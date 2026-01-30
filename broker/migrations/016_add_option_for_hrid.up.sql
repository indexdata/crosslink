CREATE OR REPLACE FUNCTION get_next_hrid(prefix text) RETURNS varchar AS $$
BEGIN
    EXECUTE format('CREATE SEQUENCE IF NOT EXISTS %I START 1', LOWER(prefix) || '_hrid_seq');
    RETURN UPPER(prefix) || '-' || nextval(LOWER(prefix) || '_hrid_seq')::TEXT;
END;
$$ LANGUAGE plpgsql;