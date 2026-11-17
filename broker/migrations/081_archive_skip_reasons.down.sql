-- Restore the original boolean functions before dropping the detailed results.
-- The caller selects through CQL first. Locking and comparing state/updated_at
-- prevents an intervening workflow update from archiving a changed request.
CREATE OR REPLACE FUNCTION archive_borrowing_request(
    p_id TEXT, p_owner TEXT, p_state TEXT, p_updated_at TIMESTAMP,
    p_cutoff TIMESTAMP, p_anonymize BOOLEAN
) RETURNS BOOLEAN
LANGUAGE plpgsql AS $$
DECLARE
    v_pr patron_request%ROWTYPE;
    v_transaction_id TEXT;
    v_bundle JSONB;
BEGIN
    SELECT * INTO v_pr FROM patron_request
     WHERE id = p_id AND side = 'borrowing' AND requester_symbol = p_owner
       AND state = p_state AND updated_at = p_updated_at
       AND updated_at <= p_cutoff
     FOR UPDATE;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    IF v_pr.requester_req_id IS NOT NULL THEN
        SELECT id INTO v_transaction_id FROM ill_transaction
         WHERE requester_request_id = v_pr.requester_req_id
           AND requester_symbol = p_owner
         FOR UPDATE;
    END IF;

    -- Skip active workflows and pending signals before child locks.
    -- Recheck after locking events in case their activity changes meanwhile.
    -- Rota audit notices target only observers, which never clear their signal.
    IF EXISTS (
        SELECT 1 FROM event
         WHERE (patron_request_id = p_id OR ill_transaction_id = v_transaction_id
                OR (v_transaction_id IS NOT NULL AND patron_request_id IN (
                    SELECT id FROM patron_request
                     WHERE side = 'lending' AND requester_symbol = p_owner
                       AND requester_req_id = v_pr.requester_req_id)))
           AND (event_status IN ('NEW', 'PROCESSING')
                OR (last_signal <> '' AND NOT (
                    event_type = 'NOTICE' AND last_signal = 'notice_created'
                    AND event_name IN ('supplier-added', 'supplier-moved'))))
    ) THEN
        RETURN FALSE;
    END IF;

    IF v_transaction_id IS NOT NULL THEN
        -- Retry reuses this transaction until its requester ID is advanced to
        -- the successor. Lending messages also resolve it by requester ID.
        -- Lock related requests without waiting: their workflows may update
        -- the transaction or this request while holding their own request lock.
        BEGIN
            PERFORM 1 FROM patron_request
             WHERE requester_symbol = p_owner
               AND ((id = v_pr.next_req_id AND side = 'borrowing'
                     AND ill_request #>> '{serviceInfo,requestType}' = 'Retry')
                    OR (side = 'lending' AND requester_req_id = v_pr.requester_req_id))
             ORDER BY id FOR UPDATE NOWAIT;
        EXCEPTION WHEN lock_not_available THEN
            RETURN FALSE;
        END;
        IF EXISTS (
            SELECT 1 FROM patron_request
             WHERE requester_symbol = p_owner AND NOT terminal_state
               AND ((id = v_pr.next_req_id AND side = 'borrowing'
                     AND ill_request #>> '{serviceInfo,requestType}' = 'Retry')
                    OR (side = 'lending' AND requester_req_id = v_pr.requester_req_id))
        ) THEN
            RETURN FALSE;
        END IF;
    END IF;

    -- Parent row locks block new FK children; child locks block changes to
    -- existing rows while their snapshots are assembled.
    -- Related lending tasks use the synthetic transaction ID, so lock and
    -- check their events by request ID even when the lending request is terminal.
    PERFORM 1 FROM event
     WHERE patron_request_id = p_id
        OR (v_transaction_id IS NOT NULL AND patron_request_id IN (
            SELECT id FROM patron_request
             WHERE side = 'lending' AND requester_symbol = p_owner
               AND requester_req_id = v_pr.requester_req_id))
     ORDER BY id FOR UPDATE;
    -- Item updates lock the item before their trigger updates this request.
    -- Never wait for them while holding the request lock: skip the candidate.
    BEGIN
        PERFORM 1 FROM item WHERE pr_id = p_id ORDER BY id FOR UPDATE NOWAIT;
    EXCEPTION WHEN lock_not_available THEN
        RETURN FALSE;
    END;
    PERFORM 1 FROM notification WHERE pr_id = p_id ORDER BY id FOR UPDATE;
    IF v_transaction_id IS NOT NULL THEN
        PERFORM 1 FROM event WHERE ill_transaction_id = v_transaction_id ORDER BY id FOR UPDATE;
        PERFORM 1 FROM located_supplier WHERE ill_transaction_id = v_transaction_id ORDER BY id FOR UPDATE;
    END IF;
    IF EXISTS (
        SELECT 1 FROM event
         WHERE (patron_request_id = p_id OR ill_transaction_id = v_transaction_id
                OR (v_transaction_id IS NOT NULL AND patron_request_id IN (
                    SELECT id FROM patron_request
                     WHERE side = 'lending' AND requester_symbol = p_owner
                       AND requester_req_id = v_pr.requester_req_id)))
           AND (event_status IN ('NEW', 'PROCESSING')
                OR (last_signal <> '' AND NOT (
                    event_type = 'NOTICE' AND last_signal = 'notice_created'
                    AND event_name IN ('supplier-added', 'supplier-moved'))))
    ) THEN
        RETURN FALSE;
    END IF;

    SELECT jsonb_build_object(
        'patronRequest', to_jsonb(v_pr),
        'patronRequestEvents', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.timestamp, e.id)
            FROM event e WHERE e.patron_request_id = p_id), '[]'::jsonb),
        'items', COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id)
            FROM item i WHERE i.pr_id = p_id), '[]'::jsonb),
        'notifications', COALESCE((SELECT jsonb_agg(to_jsonb(n) ORDER BY n.id)
            FROM notification n WHERE n.pr_id = p_id), '[]'::jsonb),
        'illTransaction', (SELECT to_jsonb(t) FROM ill_transaction t WHERE t.id = v_transaction_id),
        'illTransactionEvents', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.timestamp, e.id)
            FROM event e WHERE e.ill_transaction_id = v_transaction_id
              AND e.patron_request_id = '00000000-0000-0000-0000-000000000002'), '[]'::jsonb),
        'locatedSuppliers', COALESCE((SELECT jsonb_agg(to_jsonb(ls) ORDER BY ls.ordinal, ls.id)
            FROM located_supplier ls WHERE ls.ill_transaction_id = v_transaction_id), '[]'::jsonb)
    ) INTO v_bundle;

    IF p_anonymize THEN
        v_bundle := archive_anonymize_jsonb(v_bundle, 'bundle');
    END IF;

    INSERT INTO archived_resource
        (owner, resource_type, resource_id, schema_version, anonymized, anonymization_method, bundle)
    VALUES (p_owner, 'borrowing-patron-request', p_id, 1, p_anonymize,
        CASE WHEN p_anonymize THEN 'operational-allowlist-v1' END, v_bundle);

    -- Other patron-request events must survive deletion of the transaction.
    IF v_transaction_id IS NOT NULL THEN
        UPDATE event SET ill_transaction_id = '00000000-0000-0000-0000-000000000001'
         WHERE ill_transaction_id = v_transaction_id
           AND patron_request_id NOT IN (p_id, '00000000-0000-0000-0000-000000000002');
    END IF;
    DELETE FROM patron_request WHERE id = p_id;
    IF v_transaction_id IS NOT NULL THEN
        DELETE FROM ill_transaction WHERE id = v_transaction_id;
    END IF;
    RETURN TRUE;
END;
$$;

-- Lending archives own only the supplier PR and its children. Transaction-domain
-- events and located suppliers remain with the transaction and borrowing data.
CREATE OR REPLACE FUNCTION archive_lending_request(
    p_id TEXT, p_owner TEXT, p_state TEXT, p_updated_at TIMESTAMP,
    p_cutoff TIMESTAMP, p_anonymize BOOLEAN
) RETURNS BOOLEAN
LANGUAGE plpgsql AS $$
DECLARE
    v_pr patron_request%ROWTYPE;
    v_bundle JSONB;
BEGIN
    SELECT * INTO v_pr FROM patron_request
     WHERE id = p_id AND side = 'lending' AND supplier_symbol = p_owner
       AND state = p_state AND updated_at = p_updated_at
       AND updated_at <= p_cutoff
     FOR UPDATE;
    IF NOT FOUND THEN
        RETURN FALSE;
    END IF;

    -- Skip active workflows and pending signals before child locks.
    -- Recheck with events locked.
    IF EXISTS (
        SELECT 1 FROM event WHERE patron_request_id = p_id
          AND (event_status IN ('NEW', 'PROCESSING')
                OR last_signal <> '')
    ) THEN
        RETURN FALSE;
    END IF;

    PERFORM 1 FROM event WHERE patron_request_id = p_id ORDER BY id FOR UPDATE;
    -- Item updates lock the item before their trigger updates this request.
    -- Never wait for them while holding the request lock: skip the candidate.
    BEGIN
        PERFORM 1 FROM item WHERE pr_id = p_id ORDER BY id FOR UPDATE NOWAIT;
    EXCEPTION WHEN lock_not_available THEN
        RETURN FALSE;
    END;
    PERFORM 1 FROM notification WHERE pr_id = p_id ORDER BY id FOR UPDATE;
    IF EXISTS (
        SELECT 1 FROM event WHERE patron_request_id = p_id
          AND (event_status IN ('NEW', 'PROCESSING')
                OR last_signal <> '')
    ) THEN
        RETURN FALSE;
    END IF;

    SELECT jsonb_build_object(
        'patronRequest', to_jsonb(v_pr),
        'patronRequestEvents', COALESCE((SELECT jsonb_agg(to_jsonb(e) ORDER BY e.timestamp, e.id)
            FROM event e WHERE e.patron_request_id = p_id), '[]'::jsonb),
        'items', COALESCE((SELECT jsonb_agg(to_jsonb(i) ORDER BY i.id)
            FROM item i WHERE i.pr_id = p_id), '[]'::jsonb),
        'notifications', COALESCE((SELECT jsonb_agg(to_jsonb(n) ORDER BY n.id)
            FROM notification n WHERE n.pr_id = p_id), '[]'::jsonb)
    ) INTO v_bundle;

    IF p_anonymize THEN
        -- The shared allowlist emits nulls for absent transaction sections.
        -- Remove those sections so the lending bundle contains only its data.
        v_bundle := jsonb_strip_nulls(archive_anonymize_jsonb(v_bundle, 'bundle'));
    END IF;

    INSERT INTO archived_resource
        (owner, resource_type, resource_id, schema_version, anonymized, anonymization_method, bundle)
    VALUES (p_owner, 'lending-patron-request', p_id, 1, p_anonymize,
        CASE WHEN p_anonymize THEN 'operational-allowlist-v1' END, v_bundle);

    DELETE FROM patron_request WHERE id = p_id;
    RETURN TRUE;
END;
$$;

DROP FUNCTION archive_borrowing_request_result(TEXT, TEXT, TEXT, TIMESTAMP, TIMESTAMP, BOOLEAN);

DROP FUNCTION archive_lending_request_result(TEXT, TEXT, TEXT, TIMESTAMP, TIMESTAMP, BOOLEAN);
