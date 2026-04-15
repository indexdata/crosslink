-- when changing patron_request members, also update rows.Scan args in prcql.go
CREATE TABLE patron_request
(
    id                  VARCHAR PRIMARY KEY,
    created_at          TIMESTAMP NOT NULL DEFAULT now(),
    ill_request         jsonb,
    state               VARCHAR NOT NULL,
    side                VARCHAR NOT NULL,
    patron              VARCHAR,
    requester_symbol    VARCHAR,
    supplier_symbol     VARCHAR,
    tenant              VARCHAR,
    requester_req_id    VARCHAR,
    needs_attention     BOOLEAN NOT NULL DEFAULT false,
    last_action         VARCHAR,
    last_action_outcome VARCHAR,
    last_action_result  VARCHAR,
    items               JSONB NOT NULL DEFAULT '[]'::jsonb,
    language            regconfig NOT NULL DEFAULT 'english',
    terminal_state      BOOLEAN NOT NULL DEFAULT false,
    updated_at          TIMESTAMP
);

CREATE OR REPLACE FUNCTION get_next_hrid(prefix VARCHAR) RETURNS VARCHAR AS $$
BEGIN
    EXECUTE format('CREATE SEQUENCE IF NOT EXISTS %I START 1', LOWER(prefix) || '_hrid_seq');
    RETURN UPPER(prefix) || '-' || nextval(LOWER(prefix) || '_hrid_seq')::TEXT;
END;
$$ LANGUAGE plpgsql;

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
    direction       VARCHAR   NOT NULL DEFAULT 'sent',
    kind            VARCHAR   NOT NULL DEFAULT 'note',
    note            VARCHAR,
    cost            NUMERIC(19, 4),
    currency        VARCHAR,
    condition       VARCHAR,
    receipt         VARCHAR,
    created_at      TIMESTAMP NOT NULL DEFAULT now(),
    acknowledged_at TIMESTAMP
);

CREATE OR REPLACE FUNCTION immutable_to_timestamp(text)
RETURNS timestamp
LANGUAGE sql
IMMUTABLE STRICT
AS $$
SELECT $1::timestamp;
$$;

CREATE OR REPLACE VIEW patron_request_search_view AS
SELECT
    pr.*,
    EXISTS (
        SELECT 1
        FROM notification n
        WHERE n.pr_id = pr.id
    ) AS has_notification,
    EXISTS (
        SELECT 1
        FROM notification n
        WHERE n.pr_id = pr.id and cost is not null
    ) AS has_cost,
    EXISTS (
        SELECT 1
        FROM notification n
        WHERE n.pr_id = pr.id and acknowledged_at is null
    ) AS has_unread_notification,
    pr.ill_request -> 'serviceInfo' ->> 'serviceType' AS service_type,
    pr.ill_request -> 'serviceInfo' -> 'serviceLevel' ->> '#text' AS service_level,
    immutable_to_timestamp(pr.ill_request -> 'serviceInfo' ->> 'needBeforeDate') AS needed_at
FROM patron_request pr;
