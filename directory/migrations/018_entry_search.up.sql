ALTER TABLE entries ADD COLUMN search tsvector NOT NULL DEFAULT ''::tsvector;

-- Index owner lookups before backfill; trailing columns cover symbol aggregation.
CREATE INDEX symbols_owner_authority_symbol_idx ON symbols (owner, authority, symbol);

-- Keep construction in one place for entry writes, symbol writes, and backfill.
-- VOLATILE ensures symbol reads see changes committed while waiting for an owner lock.
CREATE FUNCTION directory_entry_search_document(
    entry_id uuid, entry_name text, entry_description text,
    entry_organization_id text, entry_email text, entry_phone_number text
) RETURNS tsvector LANGUAGE sql VOLATILE AS $$
    SELECT to_tsvector('simple'::regconfig,
        coalesce(entry_name, '') || ' ' ||
        coalesce(entry_description, '') || ' ' ||
        coalesce(entry_organization_id, '') || ' ' ||
        coalesce(entry_email, '') || ' ' ||
        coalesce(entry_phone_number, '') || ' ' ||
        coalesce((SELECT string_agg(authority || ':' || symbol || ' ' || symbol,
                                   ' ' ORDER BY authority, symbol)
                  FROM symbols WHERE owner = entry_id), '')
    );
$$;

CREATE FUNCTION directory_entry_search_on_entry() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.search := directory_entry_search_document(
        NEW.id, NEW.name, NEW.description, NEW.organization_id, NEW.email, NEW.phone_number);
    RETURN NEW;
END;
$$;

CREATE TRIGGER directory_entry_search_entry
    BEFORE INSERT OR UPDATE OF name, description, organization_id, email, phone_number
    ON entries FOR EACH ROW EXECUTE FUNCTION directory_entry_search_on_entry();

-- Serialize symbol changes with entry writes before constructing the document.
-- Lock both owners in UUID order when a symbol moves. Deleted owners (including
-- ON DELETE CASCADE) need no refresh.
CREATE FUNCTION directory_entry_search_lock_symbol_owners() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    owners uuid[];
BEGIN
    IF TG_OP = 'INSERT' THEN
        owners := ARRAY[NEW.owner];
    ELSIF TG_OP = 'DELETE' THEN
        owners := ARRAY[OLD.owner];
    ELSE
        owners := ARRAY[OLD.owner, NEW.owner];
    END IF;
    PERFORM id FROM entries WHERE id = ANY(owners) ORDER BY id FOR UPDATE;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$;

CREATE FUNCTION directory_entry_search_on_symbol() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    owners uuid[];
BEGIN
    IF TG_OP = 'INSERT' THEN
        owners := ARRAY[NEW.owner];
    ELSIF TG_OP = 'DELETE' THEN
        owners := ARRAY[OLD.owner];
    ELSE
        owners := ARRAY[OLD.owner, NEW.owner];
    END IF;
    -- Updating search alone does not fire the entry trigger, avoiding recursion.
    UPDATE entries e SET search = directory_entry_search_document(
        e.id, e.name, e.description, e.organization_id, e.email, e.phone_number)
    WHERE e.id = ANY(owners);
    RETURN NULL;
END;
$$;

CREATE TRIGGER directory_entry_search_symbol_lock
    BEFORE INSERT OR UPDATE OF owner, authority, symbol OR DELETE ON symbols
    FOR EACH ROW EXECUTE FUNCTION directory_entry_search_lock_symbol_owners();

CREATE TRIGGER directory_entry_search_symbol_refresh
    AFTER INSERT OR UPDATE OF owner, authority, symbol OR DELETE ON symbols
    FOR EACH ROW EXECUTE FUNCTION directory_entry_search_on_symbol();

UPDATE entries e SET search = directory_entry_search_document(
    e.id, e.name, e.description, e.organization_id, e.email, e.phone_number);

CREATE INDEX entries_search_idx ON entries USING gin(search);
