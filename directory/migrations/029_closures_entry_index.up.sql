-- Entry reads look up closures by owning entry, including those of ancestors
-- when resolving inherited closures.
CREATE INDEX IF NOT EXISTS closures_entry_idx ON closures (entry);
