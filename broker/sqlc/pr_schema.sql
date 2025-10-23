CREATE TABLE patron_request
(
    id                VARCHAR PRIMARY KEY,
    timestamp         TIMESTAMP NOT NULL DEFAULT now(),
    ill_request       jsonb,
    state             VARCHAR   NOT NULL,
    side              VARCHAR   NOT NULL,
    requester         VARCHAR,
    borrowing_peer_id VARCHAR,
    lending_peer_id   VARCHAR
);