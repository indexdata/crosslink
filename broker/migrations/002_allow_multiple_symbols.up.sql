ALTER TABLE located_supplier ADD COLUMN supplier_symbol VARCHAR;
UPDATE located_supplier ls SET supplier_symbol = (SELECT peer.symbol FROM peer WHERE peer.id = ls.supplier_id);
ALTER TABLE located_supplier ALTER COLUMN supplier_symbol SET NOT NULL;

CREATE TABLE symbol
(
    symbol_value VARCHAR PRIMARY KEY,
    peer_id VARCHAR   NOT NULL,
    FOREIGN KEY (peer_id) REFERENCES peer (id)
);

INSERT INTO symbol (symbol_value, peer_id) SELECT peer.symbol, peer.id FROM peer;

ALTER TABLE peer DROP CONSTRAINT IF EXISTS peer_symbol_key;
ALTER TABLE peer DROP COLUMN symbol;