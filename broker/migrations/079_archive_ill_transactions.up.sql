-- Preserve archived_ill_transactions: it contains historical legacy archives.
DROP FUNCTION archive_ill_transaction_by_date_and_status(TIMESTAMPTZ, TEXT[]);

CREATE INDEX idx_ill_archive_owner_cursor ON ill_transaction (requester_symbol, id);
CREATE INDEX idx_ill_archive_previous_request ON ill_transaction (requester_symbol, prev_requester_request_id)
    WHERE prev_requester_request_id IS NOT NULL;
CREATE INDEX idx_pr_transaction_archive_link ON patron_request (requester_symbol, requester_req_id, side);

-- The relationship between requests and transactions has no FK. Lock matching
-- transactions when creating/changing that relationship so archival cannot miss
-- an uncommitted borrowing request or related lending workflow.
CREATE FUNCTION lock_patron_request_archive_transaction() RETURNS TRIGGER
LANGUAGE plpgsql AS $$
DECLARE
    v_id TEXT;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.requester_symbol IS NOT DISTINCT FROM OLD.requester_symbol
        AND NEW.requester_req_id IS NOT DISTINCT FROM OLD.requester_req_id
        AND NEW.side IS NOT DISTINCT FROM OLD.side THEN
        RETURN NEW;
    END IF;
    FOR v_id IN SELECT id FROM ill_transaction
        WHERE requester_symbol = NEW.requester_symbol
          AND (requester_request_id = NEW.requester_req_id
               OR prev_requester_request_id = NEW.requester_req_id)
        ORDER BY id
    LOOP
        PERFORM 1 FROM ill_transaction WHERE id = v_id FOR KEY SHARE;
        IF NOT FOUND THEN
            RAISE EXCEPTION 'transaction concurrently archived; retry request mutation' USING ERRCODE = '40001';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$;

CREATE TRIGGER lock_patron_request_archive_transaction
BEFORE INSERT OR UPDATE OF requester_symbol, requester_req_id, side ON patron_request
FOR EACH ROW EXECUTE FUNCTION lock_patron_request_archive_transaction();

-- xmin detects any transaction-row change after candidate selection, including
-- changes that leave its timestamp and status unchanged. All child snapshots and
-- deletion run in this statement's transaction. Contention skips the candidate.
CREATE FUNCTION archive_ill_transaction(
    p_id TEXT, p_owner TEXT, p_version TEXT, p_cutoff TIMESTAMP, p_anonymize BOOLEAN
) RETURNS BOOLEAN
LANGUAGE plpgsql AS $$
DECLARE
    v_transaction ill_transaction%ROWTYPE;
    v_bundle JSONB;
BEGIN
    SELECT * INTO v_transaction FROM ill_transaction
     WHERE id = p_id AND requester_symbol = p_owner AND xmin::text = p_version
       AND timestamp <= p_cutoff
       AND id <> '00000000-0000-0000-0000-000000000001'
     FOR UPDATE NOWAIT;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    -- Request workflows lock requests before transactions; never wait on them.
    PERFORM 1 FROM patron_request
     WHERE requester_symbol = v_transaction.requester_symbol
       AND requester_req_id IN (v_transaction.requester_request_id, v_transaction.prev_requester_request_id)
     ORDER BY id FOR UPDATE NOWAIT;
    IF EXISTS (SELECT 1 FROM patron_request
        WHERE requester_symbol = v_transaction.requester_symbol
          AND requester_req_id IN (v_transaction.requester_request_id, v_transaction.prev_requester_request_id)
          AND (side = 'borrowing' OR NOT terminal_state))
       OR EXISTS (SELECT 1 FROM availability_attempt WHERE ill_transaction_id = p_id)
    THEN
        RETURN FALSE;
    END IF;

    -- Include related PR-domain work even if it uses the synthetic transaction.
    PERFORM 1 FROM event
     WHERE ill_transaction_id = p_id OR patron_request_id IN (
         SELECT id FROM patron_request
          WHERE requester_symbol = v_transaction.requester_symbol
            AND requester_req_id IN (v_transaction.requester_request_id, v_transaction.prev_requester_request_id))
     ORDER BY id FOR UPDATE NOWAIT;
    PERFORM 1 FROM located_supplier WHERE ill_transaction_id = p_id ORDER BY id FOR UPDATE NOWAIT;
    IF EXISTS (SELECT 1 FROM event
        WHERE (ill_transaction_id = p_id OR patron_request_id IN (
            SELECT id FROM patron_request
             WHERE requester_symbol = v_transaction.requester_symbol
               AND requester_req_id IN (v_transaction.requester_request_id, v_transaction.prev_requester_request_id)))
          AND (event_status IN ('NEW', 'PROCESSING')
               OR (last_signal <> '' AND NOT (
                   event_type = 'NOTICE' AND last_signal = 'notice_created'
                   AND event_name IN ('supplier-added', 'supplier-moved')))))
    THEN
        RETURN FALSE;
    END IF;

    SELECT jsonb_build_object(
        'illTransaction', to_jsonb(v_transaction),
        'illTransactionEvents', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.timestamp, e.id)
            FROM event e WHERE e.ill_transaction_id = p_id
              AND e.patron_request_id = '00000000-0000-0000-0000-000000000002'), '[]'::jsonb),
        'locatedSuppliers', COALESCE((SELECT jsonb_agg(to_jsonb(ls) ORDER BY ls.ordinal, ls.id)
            FROM located_supplier ls WHERE ls.ill_transaction_id = p_id), '[]'::jsonb)
    ) INTO v_bundle;
    IF p_anonymize THEN
        v_bundle := archive_anonymize_jsonb(v_bundle, 'bundle')
            - ARRAY['patronRequest', 'patronRequestEvents', 'items', 'notifications'];
    END IF;
    INSERT INTO archived_resource
        (owner, resource_type, resource_id, schema_version, anonymized, anonymization_method, bundle)
    VALUES (p_owner, 'ill-transaction', p_id, 1, p_anonymize,
        CASE WHEN p_anonymize THEN 'operational-allowlist-v1' END, v_bundle);

    -- Lending requests retain ownership of their events.
    UPDATE event SET ill_transaction_id = '00000000-0000-0000-0000-000000000001'
     WHERE ill_transaction_id = p_id
       AND patron_request_id <> '00000000-0000-0000-0000-000000000002';
    DELETE FROM ill_transaction WHERE id = p_id;
    RETURN TRUE;
EXCEPTION WHEN lock_not_available THEN
    RETURN FALSE;
END;
$$;
