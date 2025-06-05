CREATE TABLE branch_symbol
(
    symbol_value VARCHAR PRIMARY KEY,
    peer_id VARCHAR   NOT NULL,
    FOREIGN KEY (peer_id) REFERENCES peer (id)
);

-- Fetching done by peer id
CREATE INDEX IF NOT EXISTS idx_branch_symbol_peer_id ON branch_symbol (peer_id);
CREATE INDEX IF NOT EXISTS idx_symbol_peer_id ON symbol (peer_id);

-- Migrate data
CREATE TABLE tmp_symbol
(
    symbol_value VARCHAR PRIMARY KEY,
    peer_id VARCHAR   NOT NULL
);
INSERT INTO tmp_symbol (peer_id, symbol_value)
SELECT p.id, (sym ->> 'authority') || ':' ||(sym ->> 'symbol')
FROM peer p,
     jsonb_array_elements(p.custom_data -> 'symbols') AS sym;

INSERT INTO branch_symbol (symbol_value, peer_id)
SELECT sym.symbol_value, sym.peer_id
FROM symbol sym WHERE NOT EXISTS(SELECT 1 FROM tmp_symbol tmp WHERE tmp.peer_id = sym.peer_id AND tmp.symbol_value = sym.symbol_value );

TRUNCATE TABLE symbol;

INSERT INTO symbol (symbol_value, peer_id)
SELECT symbol_value, peer_id FROM tmp_symbol;

DROP TABLE tmp_symbol;
