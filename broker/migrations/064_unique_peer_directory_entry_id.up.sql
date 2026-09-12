-- Preserve local references while merging duplicate directory cache entries.
LOCK TABLE peer, symbol, branch_symbol, ill_transaction, located_supplier IN ACCESS EXCLUSIVE MODE;

CREATE TEMP TABLE duplicate_directory_peers ON COMMIT DROP AS
SELECT id,
       first_value(id) OVER (
           PARTITION BY custom_data ->> 'id'
           ORDER BY refresh_time DESC, id
       ) AS survivor_id
FROM peer
WHERE custom_data ->> 'id' IS NOT NULL;

-- Keep the freshest entry's configuration and combine circulation counters.
UPDATE peer p
SET loans_count = totals.loans_count,
    borrows_count = totals.borrows_count
FROM (
    SELECT d.survivor_id, sum(p.loans_count) AS loans_count, sum(p.borrows_count) AS borrows_count
    FROM duplicate_directory_peers d JOIN peer p ON p.id = d.id
    GROUP BY d.survivor_id
    HAVING count(*) > 1
) totals
WHERE p.id = totals.survivor_id;

UPDATE symbol s SET peer_id = d.survivor_id
FROM duplicate_directory_peers d WHERE s.peer_id = d.id AND d.id <> d.survivor_id;
UPDATE branch_symbol s SET peer_id = d.survivor_id
FROM duplicate_directory_peers d WHERE s.peer_id = d.id AND d.id <> d.survivor_id;
UPDATE ill_transaction t SET requester_id = d.survivor_id
FROM duplicate_directory_peers d WHERE t.requester_id = d.id AND d.id <> d.survivor_id;
UPDATE located_supplier s SET supplier_id = d.survivor_id
FROM duplicate_directory_peers d WHERE s.supplier_id = d.id AND d.id <> d.survivor_id;
DELETE FROM peer p USING duplicate_directory_peers d
WHERE p.id = d.id AND d.id <> d.survivor_id;

DROP INDEX peer_directory_entry_id_idx;
CREATE UNIQUE INDEX peer_directory_entry_id_idx ON peer ((custom_data ->> 'id'));
