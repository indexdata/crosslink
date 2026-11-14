-- Raises the HRID sequence for prefix so get_next_hrid never returns an ID at
-- or below used, which an imported request already holds. The sequence never
-- moves backwards. setval is not transactional, so a read followed by setval
-- must not interleave with another advance or with nextval: a value read
-- earlier would overwrite a higher one. advance_hrid therefore holds an
-- exclusive advisory lock on the sequence until commit, and get_next_hrid holds
-- a shared one, so request creation only waits while an import advances the
-- same prefix.
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

CREATE OR REPLACE FUNCTION get_next_hrid(prefix text) RETURNS varchar AS $$
BEGIN
    PERFORM pg_advisory_xact_lock_shared(hashtextextended('broker:hrid-seq:' || LOWER(prefix) || '_hrid_seq', 0));
    EXECUTE format('CREATE SEQUENCE IF NOT EXISTS %I START 1', LOWER(prefix) || '_hrid_seq');
    RETURN UPPER(prefix) || '-' || nextval(LOWER(prefix) || '_hrid_seq')::TEXT;
END;
$$ LANGUAGE plpgsql;
