CREATE TABLE archived_ill_transactions
(
    ill_transaction JSONB NOT NULL,
    events JSONB NOT NULL,
    located_suppliers JSONB NOT NULL
);

CREATE OR REPLACE FUNCTION archive_ill_transaction_by_date_and_status(
    t_cut_off_date TIMESTAMPTZ,
    t_status_list TEXT[]
)
    RETURNS INT AS $$
DECLARE
    v_deleted_ids TEXT[];
    v_deleted_count INT := 0;
    lock_id BIGINT := 8372910465;
    lock_acquired BOOLEAN;
BEGIN
    SELECT pg_try_advisory_lock(lock_id) INTO lock_acquired;

    IF NOT lock_acquired THEN
        RAISE NOTICE 'Function archive_ill_transaction_by_date_and_status() is already running. Exiting.';
        RETURN 0;
    END IF;

    SELECT array_agg(id) INTO v_deleted_ids
    FROM ill_transaction
    WHERE timestamp <= t_cut_off_date
      AND last_supplier_status = ANY(t_status_list);

    -- If no ill transactions match the criteria, exit early
    IF v_deleted_ids IS NULL OR array_length(v_deleted_ids, 1) IS NULL THEN
        RAISE NOTICE 'No ILL transactions found matching date % and statuses %', t_cut_off_date, t_status_list;
        RETURN 0;
    END IF;

    INSERT INTO archived_ill_transactions (ill_transaction, events, located_suppliers)
    SELECT
        row_to_json(t)::jsonb as transaction_json,
        COALESCE(pe.events, '[]'::jsonb) as events_json,
        COALESCE(pls.contacts, '[]'::jsonb) as located_suppliers_json
    FROM
        ill_transaction AS t
            LEFT JOIN LATERAL (
            SELECT
                e.ill_transaction_id,
                jsonb_agg(row_to_json(e)) AS events
            FROM
                event AS e
            WHERE
                e.ill_transaction_id = t.id
            GROUP BY
                e.ill_transaction_id
            ) AS pe ON pe.ill_transaction_id = t.id
            LEFT JOIN LATERAL (
            SELECT
                ls.ill_transaction_id,
                jsonb_agg(row_to_json(ls)) AS contacts
            FROM
                located_supplier AS ls
            WHERE
                ls.ill_transaction_id = t.id
            GROUP BY
                ls.ill_transaction_id
            ) AS pls ON pls.ill_transaction_id = t.id
    WHERE t.id = ANY(v_deleted_ids);

    DELETE FROM located_supplier
    WHERE ill_transaction_id = ANY(v_deleted_ids);
    GET DIAGNOSTICS v_deleted_count = ROW_COUNT;
    RAISE NOTICE 'Deleted % located_supplier rows.', v_deleted_count;

    DELETE FROM event
    WHERE ill_transaction_id = ANY(v_deleted_ids);
    GET DIAGNOSTICS v_deleted_count = ROW_COUNT;
    RAISE NOTICE 'Deleted % event rows.', v_deleted_count;

    DELETE FROM ill_transaction
    WHERE id = ANY(v_deleted_ids);
    GET DIAGNOSTICS v_deleted_count = ROW_COUNT;
    RAISE NOTICE 'Deleted % ill_transaction rows.', v_deleted_count;

    RETURN v_deleted_count;
EXCEPTION
    WHEN OTHERS THEN
        PERFORM pg_advisory_unlock(lock_id);
        RAISE;
END;
$$ LANGUAGE plpgsql;