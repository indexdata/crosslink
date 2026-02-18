CREATE TABLE item
(
    id          VARCHAR PRIMARY KEY,
    pr_id       VARCHAR   NOT NULL REFERENCES patron_request (id),
    barcode     VARCHAR   NOT NULL,
    call_number VARCHAR,
    title       VARCHAR,
    item_id     VARCHAR,
    created_at  TIMESTAMP NOT NULL DEFAULT now()
);

CREATE TABLE notification
(
    id              VARCHAR PRIMARY KEY,
    pr_id           VARCHAR   NOT NULL REFERENCES patron_request (id),
    from_symbol     VARCHAR   NOT NULL,
    to_symbol       VARCHAR   NOT NULL,
    side            VARCHAR   NOT NULL,
    note            VARCHAR,
    cost            NUMERIC(19, 4),
    currency        VARCHAR,
    condition       VARCHAR,
    receipt         VARCHAR,
    created_at      TIMESTAMP NOT NULL DEFAULT now(),
    acknowledged_at TIMESTAMP
);
