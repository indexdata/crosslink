CREATE TABLE pull_slip
(
    id              VARCHAR PRIMARY KEY,
    created_at      TIMESTAMP   NOT NULL DEFAULT now(),
    generated_at    TIMESTAMP,
    type            VARCHAR     NOT NULL,
    owner           VARCHAR     NOT NULL,
    search_criteria VARCHAR     NOT NULL,
    pdf_bytes       BYTEA
);

