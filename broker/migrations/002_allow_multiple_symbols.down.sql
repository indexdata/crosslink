ALTER TABLE located_supplier DROP COLUMN supplier_symbol;

ALTER TABLE peer ADD COLUMN symbol VARCHAR;
UPDATE peer SET symbol = (SELECT max(symbol_value) FROM symbol WHERE peer.id = symbol.peer_id);

ALTER TABLE peer ALTER COLUMN symbol SET NOT NULL;
CREATE UNIQUE INDEX peer_symbol_key ON peer(symbol);

DROP TABLE symbol;