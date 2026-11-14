-- Raises the HRID sequence for prefix so get_next_hrid never returns an ID at
-- or below used, which an imported request already holds. The sequence never
-- moves backwards. setval is not transactional, so concurrent advances of one
-- sequence are serialized until commit; otherwise a lower value read earlier
-- could overwrite a higher one.
CREATE OR REPLACE FUNCTION advance_hrid(prefix text, used bigint) RETURNS void AS $$
DECLARE
    seq text := LOWER(prefix) || '_hrid_seq';
    current_value bigint;
    called boolean;
BEGIN
    PERFORM pg_advisory_xact_lock(hashtextextended('broker:hrid-seq:' || seq, 0));
    EXECUTE format('CREATE SEQUENCE IF NOT EXISTS %I START 1', seq);
    EXECUTE format('SELECT last_value, is_called FROM %I', seq) INTO current_value, called;
    IF NOT called OR current_value < used THEN
        PERFORM setval(quote_ident(seq)::regclass, GREATEST(current_value, used));
    END IF;
END;
$$ LANGUAGE plpgsql;
