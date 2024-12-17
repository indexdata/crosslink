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

CREATE TABLE event_type
(
    type        VARCHAR PRIMARY KEY,
    retry_count INT NOT NULL DEFAULT 0
);

CREATE TABLE event
(
    id                 VARCHAR PRIMARY KEY,
    ill_transaction_id VARCHAR   NOT NULL,
    event_type         VARCHAR   NOT NULL,
    event_status       VARCHAR   NOT NULL,
    event_data         jsonb,
    result_data        jsonb,
    created_at         TIMESTAMP NOT NULL,
    FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id),
    FOREIGN KEY (event_type) REFERENCES event_type (type)
)

