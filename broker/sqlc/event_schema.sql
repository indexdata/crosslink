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
