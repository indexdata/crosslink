CREATE TABLE patron_request
(
    id                VARCHAR PRIMARY KEY,
    timestamp         TIMESTAMP NOT NULL DEFAULT now(),
    ill_request       jsonb,
    state             VARCHAR   NOT NULL,
    side              VARCHAR   NOT NULL,
    patron            VARCHAR,
    requester_symbol  VARCHAR,
    supplier_symbol   VARCHAR,
    tenant            VARCHAR,
    requester_req_id  VARCHAR
);