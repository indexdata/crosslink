CREATE TABLE peer
(
    id      VARCHAR PRIMARY KEY,
    symbol  VARCHAR NOT NULL,
    name    VARCHAR NOT NULL,
    address VARCHAR
);

CREATE TABLE ill_transaction
(
    id                        VARCHAR PRIMARY KEY,
    timestamp                 TIMESTAMP NOT NULL,
    requester_symbol          VARCHAR,
    requester_id              VARCHAR,
    last_requester_action     VARCHAR,
    previous_requester_action VARCHAR,
    supplier_symbol           VARCHAR,
    requester_request_id      VARCHAR,
    supplier_request_id       VARCHAR,
    ill_transaction_data      jsonb     NOT NULL,
    FOREIGN KEY (requester_id) REFERENCES peer (id)
);

CREATE TABLE located_supplier
(
    id                 VARCHAR PRIMARY KEY,
    ill_transaction_id VARCHAR NOT NULL,
    supplier_id        VARCHAR NOT NULL,
    ordinal            INT     NOT NULL DEFAULT 0,
    supplier_status    VARCHAR,
    previous_action    VARCHAR,
    previous_status    VARCHAR,
    last_action        VARCHAR,
    last_status        VARCHAR,
    local_id           VARCHAR,
    FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id),
    FOREIGN KEY (supplier_id) REFERENCES peer (id)
);
