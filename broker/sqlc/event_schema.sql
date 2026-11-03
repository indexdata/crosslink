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
    parent_id          VARCHAR,
    event_type         VARCHAR   NOT NULL,
    event_name         VARCHAR   NOT NULL,
    event_status       VARCHAR   NOT NULL,
    event_data         jsonb,
    result_data        jsonb,
    last_signal        VARCHAR   NOT NULL,
    patron_request_id  VARCHAR   NOT NULL DEFAULT '00000000-0000-0000-0000-000000000002',
    FOREIGN KEY (ill_transaction_id) REFERENCES ill_transaction (id) ON DELETE CASCADE,
    FOREIGN KEY (patron_request_id) REFERENCES patron_request (id) ON DELETE CASCADE,
    FOREIGN KEY (event_name) REFERENCES event_config (event_name)
);

-- A record survives the consumer claim until all handlers finish their handoff.
-- Claimed records are deliberately retained on interruption; do not replay them
-- automatically because a handler may already have performed external effects.
CREATE TABLE event_consumer_work (
    event_id VARCHAR NOT NULL REFERENCES event(id) ON DELETE CASCADE,
    signal VARCHAR NOT NULL,
    claimed BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (event_id, signal)
);
