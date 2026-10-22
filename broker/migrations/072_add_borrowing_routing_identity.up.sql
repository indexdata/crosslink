LOCK TABLE patron_request IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    conflicts TEXT;
BEGIN
    WITH duplicate_identities AS (
        SELECT requester_req_id, ARRAY_AGG(id ORDER BY id) AS ids
        FROM patron_request
        WHERE side = 'borrowing'
          AND requester_req_id IS NOT NULL
        GROUP BY requester_req_id
        HAVING COUNT(*) > 1
    )
    SELECT STRING_AGG(
        FORMAT('requester_req_id=%L ids=%s', requester_req_id, ids::TEXT),
        '; ' ORDER BY requester_req_id
    )
    INTO conflicts
    FROM duplicate_identities;

    IF conflicts IS NOT NULL THEN
        RAISE EXCEPTION 'Duplicate borrowing routing identities already exist: %', conflicts
            USING ERRCODE = 'unique_violation';
    END IF;
END;
$$;

CREATE UNIQUE INDEX idx_patron_request_borrowing_routing_identity
    ON patron_request (requester_req_id)
    WHERE side = 'borrowing';
