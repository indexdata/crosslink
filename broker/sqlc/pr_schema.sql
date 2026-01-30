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

CREATE OR REPLACE FUNCTION get_next_hrid(prefix VARCHAR) RETURNS VARCHAR AS $$
BEGIN
    EXECUTE format('CREATE SEQUENCE IF NOT EXISTS %I START 1', LOWER(prefix) || '_hrid_seq');
    RETURN UPPER(prefix) || '-' || nextval(LOWER(prefix) || '_hrid_seq')::TEXT;
END;
$$ LANGUAGE plpgsql;