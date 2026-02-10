CREATE ROLE crosslink_broker PASSWORD 'tenant' NOSUPERUSER NOCREATEDB INHERIT LOGIN;
CREATE SCHEMA IF NOT EXISTS crosslink_broker;

SET search_path TO crosslink_broker;

CREATE TABLE peer
(
    id             VARCHAR PRIMARY KEY,
    symbol         VARCHAR   NOT NULL,
    name           VARCHAR   NOT NULL,
    refresh_policy VARCHAR   NOT NULL,
    refresh_time   TIMESTAMP NOT NULL DEFAULT now(),
    url            VARCHAR   NOT NULL,
    loans_count    INTEGER   NOT NULL DEFAULT 0,
    borrows_count  INTEGER   NOT NULL DEFAULT 0,
    vendor         VARCHAR   NOT NULL,
    UNIQUE (symbol)
);

CREATE TABLE ill_transaction
(
    id                        VARCHAR PRIMARY KEY,
    timestamp                 TIMESTAMP NOT NULL,
    requester_symbol          VARCHAR,
    requester_id              VARCHAR,
    last_requester_action     VARCHAR,
    prev_requester_action     VARCHAR,
    supplier_symbol           VARCHAR,
    requester_request_id      VARCHAR,
    prev_requester_request_id VARCHAR,
    supplier_request_id       VARCHAR,
    last_supplier_status      VARCHAR,
    prev_supplier_status      VARCHAR,
    ill_transaction_data      jsonb NOT NULL,
    FOREIGN KEY (requester_id) REFERENCES peer (id),
    UNIQUE (requester_request_id)
);

CREATE TABLE event_config
(
    event_name  VARCHAR PRIMARY KEY,
    event_type  VARCHAR NOT NULL,
    retry_count INT     NOT NULL DEFAULT 0
);

INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('request-received', 'NOTICE', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('request-terminated', 'NOTICE', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('locate-suppliers', 'TASK', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('select-supplier', 'TASK', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('supplier-msg-received', 'NOTICE', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('message-requester', 'TASK', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('requester-msg-received', 'NOTICE', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('message-supplier', 'TASK', 1);
INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('confirm-requester-msg', 'TASK', 1);

CREATE TABLE event
(
    id                 VARCHAR PRIMARY KEY,
    timestamp          TIMESTAMP NOT NULL DEFAULT now(),
    ill_transaction_id VARCHAR   NOT NULL,
    parent_id          VARCHAR,
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
    id                  VARCHAR PRIMARY KEY,
    ill_transaction_id  VARCHAR NOT NULL,
    supplier_id         VARCHAR NOT NULL,
    ordinal             INT     NOT NULL DEFAULT 0,
    supplier_status     VARCHAR,
    prev_action         VARCHAR,
    prev_status         VARCHAR,
    last_action         VARCHAR,
    last_status         VARCHAR,
    local_id            VARCHAR,
    prev_reason         VARCHAR,
    last_reason         VARCHAR,
    supplier_request_id VARCHAR,
    FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id),
    FOREIGN KEY (supplier_id) REFERENCES peer (id)
);