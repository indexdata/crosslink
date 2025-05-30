CREATE TABLE branch_symbol
(
    symbol_value VARCHAR PRIMARY KEY,
    peer_id VARCHAR   NOT NULL,
    FOREIGN KEY (peer_id) REFERENCES peer (id)
);

-- Fetching done by peer id
CREATE INDEX IF NOT EXISTS idx_branch_symbol_peer_id ON branch_symbol (peer_id);
CREATE INDEX IF NOT EXISTS idx_symbol_peer_id ON symbol (peer_id);