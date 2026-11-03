CREATE INDEX idx_pr_borrowing_archive_cursor
    ON patron_request (requester_symbol, created_at, id)
    WHERE side = 'borrowing' AND ill_request IS NOT NULL;

CREATE INDEX idx_pr_lending_archive_cursor
    ON patron_request (supplier_symbol, created_at, id)
    WHERE side = 'lending' AND ill_request IS NOT NULL;

CREATE TABLE archived_resource (
    owner TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    archived_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    schema_version INTEGER NOT NULL,
    anonymized BOOLEAN NOT NULL,
    anonymization_method TEXT,
    bundle JSONB NOT NULL,
    PRIMARY KEY (resource_type, resource_id)
);

-- Only explicitly approved scalar fields survive. Unknown keys and structured
-- values are discarded, including identifiers represented as numbers or keys.
CREATE FUNCTION archive_pick_scalars(value JSONB, allowed_fields TEXT[]) RETURNS JSONB
LANGUAGE sql IMMUTABLE AS $$
    SELECT COALESCE(jsonb_object_agg(key, val), '{}'::jsonb)
    FROM jsonb_each(CASE WHEN jsonb_typeof(value) = 'object' THEN value ELSE '{}'::jsonb END) AS entry(key, val)
    WHERE key = ANY(allowed_fields)
      AND jsonb_typeof(val) IN ('string', 'number', 'boolean', 'null');
$$;

-- Context-specific allowlists keep arbitrary embedded payloads out of anonymous
-- archives. Adding a live column cannot implicitly expand the retained data.
CREATE FUNCTION archive_anonymize_jsonb(value JSONB, kind TEXT) RETURNS JSONB
LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE
    result JSONB;
    allowed_fields TEXT[];
BEGIN
    IF value IS NULL OR value = 'null'::jsonb THEN
        RETURN 'null'::jsonb;
    END IF;
    IF jsonb_typeof(value) = 'array' THEN
        SELECT COALESCE(jsonb_agg(archive_anonymize_jsonb(val, kind) ORDER BY ordinal), '[]'::jsonb)
          INTO result FROM jsonb_array_elements(value) WITH ORDINALITY AS entry(val, ordinal);
        RETURN result;
    END IF;
    IF jsonb_typeof(value) <> 'object' THEN
        RETURN 'null'::jsonb;
    END IF;
    CASE kind
        WHEN 'bundle' THEN
            RETURN jsonb_build_object(
                'patronRequest', archive_anonymize_jsonb(value->'patronRequest', 'patron-request'),
                'patronRequestEvents', archive_anonymize_jsonb(value->'patronRequestEvents', 'event'),
                'illTransaction', archive_anonymize_jsonb(value->'illTransaction', 'transaction'),
                'illTransactionEvents', archive_anonymize_jsonb(value->'illTransactionEvents', 'event'),
                'items', archive_anonymize_jsonb(value->'items', 'item'),
                'notifications', archive_anonymize_jsonb(value->'notifications', 'notification'),
                'locatedSuppliers', archive_anonymize_jsonb(value->'locatedSuppliers', 'located-supplier'));
        WHEN 'patron-request' THEN
            allowed_fields := ARRAY['created_at', 'updated_at', 'state', 'side', 'requester_symbol',
                'supplier_symbol', 'tenant', 'needs_attention', 'last_action', 'last_action_outcome',
                'terminal_state', 'state_model', 'due_at'];
        WHEN 'transaction' THEN
            allowed_fields := ARRAY['timestamp', 'requester_symbol', 'supplier_symbol',
                'last_requester_action', 'prev_requester_action', 'last_supplier_status', 'prev_supplier_status'];
        WHEN 'event' THEN
            allowed_fields := ARRAY['timestamp', 'event_type', 'event_name', 'event_status', 'last_signal'];
        WHEN 'event-payload' THEN
            allowed_fields := ARRAY['action'];
        WHEN 'action-result' THEN
            allowed_fields := ARRAY['outcome', 'toState', 'continuationAllowed'];
        WHEN 'service-info' THEN
            allowed_fields := ARRAY['serviceType', 'requestType'];
        WHEN 'item' THEN
            allowed_fields := ARRAY['created_at', 'lms_status', 'lms_due_date'];
        WHEN 'notification' THEN
            allowed_fields := ARRAY['from_symbol', 'to_symbol', 'direction', 'kind', 'cost',
                'currency', 'receipt', 'created_at', 'acknowledged_at'];
        WHEN 'located-supplier' THEN
            allowed_fields := ARRAY['supplier_symbol', 'ordinal', 'supplier_status', 'prev_action',
                'prev_status', 'last_action', 'last_status', 'local_supplier'];
        ELSE
            RETURN '{}'::jsonb;
    END CASE;
    result := archive_pick_scalars(value, allowed_fields);
    CASE kind
        WHEN 'patron-request' THEN
            result := result || jsonb_build_object('ill_request', jsonb_build_object(
                'serviceInfo', archive_anonymize_jsonb(value#>'{ill_request,serviceInfo}', 'service-info')));
        WHEN 'transaction' THEN
            result := result || jsonb_build_object('ill_transaction_data', jsonb_build_object(
                'serviceInfo', archive_anonymize_jsonb(value#>'{ill_transaction_data,serviceInfo}', 'service-info')));
        WHEN 'event' THEN
            result := result || jsonb_build_object(
                'event_data', archive_anonymize_jsonb(value->'event_data', 'event-payload'),
                'result_data', archive_anonymize_jsonb(value->'result_data', 'event-payload'));
        WHEN 'event-payload' THEN
            result := result || jsonb_build_object(
                'actionResult', archive_anonymize_jsonb(value->'actionResult', 'action-result'));
        ELSE
            NULL;
    END CASE;
    RETURN jsonb_strip_nulls(result);
END;
$$;

-- The caller selects through CQL first. Locking and comparing state/updated_at
-- prevents an intervening workflow update from archiving a changed request.
CREATE FUNCTION archive_borrowing_request(
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

    -- Parent row locks block new FK children; child locks block changes to
    -- existing rows while their snapshots are assembled.
    PERFORM 1 FROM event WHERE patron_request_id = p_id ORDER BY id FOR UPDATE;
    PERFORM 1 FROM item WHERE pr_id = p_id ORDER BY id FOR UPDATE;
    PERFORM 1 FROM notification WHERE pr_id = p_id ORDER BY id FOR UPDATE;
    IF v_transaction_id IS NOT NULL THEN
        PERFORM 1 FROM event WHERE ill_transaction_id = v_transaction_id ORDER BY id FOR UPDATE;
        PERFORM 1 FROM located_supplier WHERE ill_transaction_id = v_transaction_id ORDER BY id FOR UPDATE;
    END IF;
    IF EXISTS (
        SELECT 1 FROM event
         WHERE (patron_request_id = p_id OR ill_transaction_id = v_transaction_id)
           AND event_status IN ('NEW', 'PROCESSING')
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
CREATE FUNCTION archive_lending_request(
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

    PERFORM 1 FROM event WHERE patron_request_id = p_id ORDER BY id FOR UPDATE;
    PERFORM 1 FROM item WHERE pr_id = p_id ORDER BY id FOR UPDATE;
    PERFORM 1 FROM notification WHERE pr_id = p_id ORDER BY id FOR UPDATE;
    IF EXISTS (
        SELECT 1 FROM event WHERE patron_request_id = p_id
          AND event_status IN ('NEW', 'PROCESSING')
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
