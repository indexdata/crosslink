CREATE TABLE peer
(
    id      VARCHAR PRIMARY KEY,
    symbol  VARCHAR NOT NULL,
    name    VARCHAR NOT NULL,
    address VARCHAR
);

CREATE TABLE ill_transaction
(
    id                   VARCHAR PRIMARY KEY,
    timestamp            TIMESTAMP NOT NULL,
    requester_symbol     VARCHAR,
    requester_id         VARCHAR,
    requester_action     VARCHAR,
    supplier_symbol      VARCHAR,
    state                VARCHAR,
    requester_request_id VARCHAR,
    supplier_request_id  VARCHAR,
    ill_transaction_data jsonb     NOT NULL,
    FOREIGN KEY (requester_id) REFERENCES peer (id)
);

CREATE TABLE event_config
(
    event_name  VARCHAR PRIMARY KEY,
    event_type  VARCHAR NOT NULL,
    retry_count INT     NOT NULL DEFAULT 0
);

CREATE TABLE event
(
    id                 VARCHAR PRIMARY KEY,
    timestamp          TIMESTAMP NOT NULL,
    ill_transaction_id VARCHAR   NOT NULL,
    event_type         VARCHAR   NOT NULL,
    event_name         VARCHAR   NOT NULL,
    event_status       VARCHAR   NOT NULL,
    event_data         jsonb,
    result_data        jsonb,
    FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id),
    FOREIGN KEY (event_name) REFERENCES event_config (event_name)
);

CREATE TABLE located_supplier
(
    id                 VARCHAR PRIMARY KEY,
    ill_transaction_id VARCHAR NOT NULL,
    supplier_id        VARCHAR NOT NULL,
    ordinal            INT     NOT NULL DEFAULT 0,
    supplier_status    VARCHAR,
    FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id),
    FOREIGN KEY (supplier_id) REFERENCES peer (id)
);

