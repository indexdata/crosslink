-- Merged peers cannot be separated again; retain their references and counters.
DROP INDEX peer_directory_entry_id_idx;
CREATE INDEX peer_directory_entry_id_idx ON peer ((custom_data ->> 'id'));
