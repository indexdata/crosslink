-- name: GetEventConfig :one
SELECT sqlc.embed(event_config) FROM event_config
WHERE event_name = $1 LIMIT 1;

-- name: ListEventConfigs :many
SELECT sqlc.embed(event_config) FROM event_config
ORDER BY event_name;

-- name: CreateEventConfig :one
INSERT INTO event_config (
    event_name, retry_count
) VALUES (
             $1, $2
         )
RETURNING sqlc.embed(event_config);

-- name: DeleteEventConfig :exec
DELETE FROM event_config
WHERE event_name = $1;

-- name: GetEvent :one
SELECT sqlc.embed(event) FROM event
WHERE id = $1 LIMIT 1;

-- name: GetEventForUpdate :one
SELECT sqlc.embed(event) FROM event
WHERE id = $1
    FOR UPDATE
LIMIT 1;

-- name: GetOlderIncompleteEvent :one
WITH RECURSIVE ancestors AS (
    SELECT parent_id
    FROM event
    WHERE id = sqlc.arg(id)
    UNION ALL
    SELECT parent.parent_id
    FROM event parent
        JOIN ancestors ancestor ON parent.id = ancestor.parent_id
    WHERE ancestor.parent_id IS NOT NULL
)
SELECT sqlc.embed(event)
FROM event
WHERE event.id <> sqlc.arg(id)
  AND event.event_type = sqlc.arg(event_type)
  AND event.event_name = sqlc.arg(event_name)
  AND event.event_status IN ('NEW', 'PROCESSING')
  AND (event.timestamp, event.id) < (sqlc.arg(timestamp), sqlc.arg(id))
  AND event.id NOT IN (SELECT parent_id FROM ancestors WHERE parent_id IS NOT NULL)
  AND event.patron_request_id = sqlc.arg(patron_request_id)
  AND event.ill_transaction_id = sqlc.arg(ill_transaction_id)
ORDER BY event.timestamp, event.id
LIMIT 1;

-- name: ClaimEventForSignal :one
UPDATE event
SET last_signal = ''
WHERE last_signal = $2 AND event.id = $1
RETURNING sqlc.embed(event);

-- name: GetIllTransactionEvents :many
SELECT sqlc.embed(event), COUNT(*) OVER () as full_count
FROM event
WHERE ill_transaction_id = $1
ORDER BY timestamp;

-- name: GetPatronRequestEvents :many
SELECT sqlc.embed(event)
FROM event
WHERE patron_request_id = $1
ORDER BY timestamp;

-- name: GetBatchActionEvents :many
SELECT sqlc.embed(event)
FROM event
WHERE event_name IN ('invoke-batch-action', 'invoke-background-action')
  AND event_data -> 'batchActionData' ->> 'taskId' = sqlc.arg(task_id)::text
ORDER BY timestamp DESC;

-- name: SaveEvent :one
INSERT INTO event (
    id, timestamp, ill_transaction_id, parent_id, event_type, event_name, event_status, event_data, result_data, last_signal, patron_request_id
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
         )
ON CONFLICT (id) DO UPDATE
    SET timestamp = EXCLUDED.timestamp,
    ill_transaction_id = EXCLUDED.ill_transaction_id,
    parent_id = EXCLUDED.parent_id,
    event_name = EXCLUDED.event_name,
    event_type = EXCLUDED.event_type,
    event_status = EXCLUDED.event_status,
    event_data = EXCLUDED.event_data,
    result_data = EXCLUDED.result_data,
    last_signal = EXCLUDED.last_signal,
    patron_request_id = EXCLUDED.patron_request_id
RETURNING sqlc.embed(event);

-- name: DeleteEvent :exec
DELETE FROM event
WHERE id = $1;

-- name: HasActiveBatchActionEvents :one
SELECT EXISTS (
    SELECT 1
    FROM batch_action_run
    WHERE scheduled_task_id = sqlc.arg(task_id)::text AND completed_at IS NULL
    UNION ALL
    SELECT 1
    FROM event
    WHERE event_name IN ('invoke-batch-action', 'invoke-background-action')
      AND event_data -> 'batchActionData' ->> 'taskId' = sqlc.arg(task_id)::text
      AND event_status IN ('NEW', 'PROCESSING')
);

-- name: DeleteBatchActionEvents :exec
DELETE FROM event
WHERE event_name IN ('invoke-batch-action', 'invoke-background-action')
  AND event_data -> 'batchActionData' ->> 'taskId' = sqlc.arg(task_id)::text;

-- name: DeleteOldBatchActionRunEvents :exec
    WITH retained_runs AS (
        SELECT id
        FROM event
        WHERE event_name = 'invoke-batch-action'
          AND event_status IN ('SUCCESS', 'ERROR', 'PROBLEM')
          AND id <> sqlc.arg(current_event_id)::text
          AND event_data -> 'batchActionData' ->> 'taskId' = sqlc.arg(task_id)::text
        ORDER BY timestamp DESC, id DESC
        LIMIT sqlc.arg(retention)::int
    ),
    old_runs AS (
        SELECT id
        FROM event
        WHERE event_name = 'invoke-batch-action'
          AND event_status IN ('SUCCESS', 'ERROR', 'PROBLEM')
          AND id <> sqlc.arg(current_event_id)::text
          AND event_data -> 'batchActionData' ->> 'taskId' = sqlc.arg(task_id)::text
          AND id NOT IN (SELECT id FROM retained_runs)
          AND id NOT IN (SELECT id FROM batch_action_run WHERE completed_at IS NULL)
    ),
    cleared_parent_refs AS (
        UPDATE event
        SET parent_id = NULL
        WHERE parent_id IN (SELECT id FROM event
            WHERE patron_request_id = sqlc.arg(patron_request_id)::text AND
                (id IN (SELECT id FROM old_runs) OR
            parent_id IN (SELECT id FROM old_runs)))
    )
    DELETE FROM event
    WHERE patron_request_id = sqlc.arg(patron_request_id)::text AND
        (id IN (SELECT id FROM old_runs) OR
          parent_id IN (SELECT id FROM old_runs));

-- name: ReserveBatchActionRun :one
INSERT INTO batch_action_run (id, scheduled_task_id, owner, action_name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (owner, action_name) WHERE completed_at IS NULL DO NOTHING
RETURNING id;

-- name: AddBatchActionRunEvent :exec
INSERT INTO batch_action_run_event (event_id, run_id) VALUES ($1, $2);

-- name: RecordBatchEmailIntent :execrows
INSERT INTO batch_action_email_delivery (run_id, selected_request_ids, queue_printed_actions)
SELECT id, sqlc.arg(selected_request_ids)::varchar[], sqlc.arg(queue_printed_actions)::boolean
FROM batch_action_run
WHERE id = sqlc.arg(run_id) AND action_name = 'email-pullslips' AND completed_at IS NULL
ON CONFLICT (run_id) DO NOTHING;

-- name: RecordBatchEmailAcceptance :execrows
UPDATE batch_action_email_delivery
SET accepted_at = COALESCE(accepted_at, now())
WHERE run_id = $1;

-- name: GetBatchActionAttempt :one
SELECT attempt_id FROM batch_action_run_event WHERE event_id = $1;

-- name: GetBatchTaskClaimEligibility :one
SELECT e.event_status = 'NEW' AND e.last_signal = $2
    AND link.attempt_id IS NULL AND link.recovery_blocked_at IS NULL
FROM batch_action_run_event link
JOIN event e ON e.id = link.event_id
WHERE link.event_id = $1;

-- name: AssignBatchActionAttempt :execrows
UPDATE batch_action_run_event
SET attempt_id = sqlc.narg(attempt_id), attempt_claimed_at = clock_timestamp(), attempt_lock_id = sqlc.narg(attempt_lock_id)
WHERE event_id = $1 AND attempt_id IS NULL AND recovery_blocked_at IS NULL;

-- name: LockBatchActionRunForEvent :one
SELECT batch_action_run.id, batch_action_run.completed_at
FROM batch_action_run_event
JOIN batch_action_run ON batch_action_run.id = batch_action_run_event.run_id
WHERE batch_action_run_event.event_id = $1
FOR UPDATE OF batch_action_run;

-- name: BatchActionParentIsProcessing :one
SELECT event_status = 'PROCESSING' FROM event WHERE id = $1;

-- name: FinishBatchActionRunIfIdle :exec
SELECT finish_batch_action_run_if_idle(run_id)
FROM batch_action_run_event
WHERE event_id = $1;

-- name: UpdateEventLifecycle :one
UPDATE event SET last_signal = $3, event_status = $2
WHERE id = $1
RETURNING sqlc.embed(event);

-- name: GetLatestRequestEventByAction :one
SELECT sqlc.embed(event) FROM event
WHERE ill_transaction_id = sqlc.arg(IllTransactionID) AND event_name = 'requester-msg-received' AND
    (event_data -> 'incomingMessage' -> 'requestingAgencyMessage' ->> 'action')::text = sqlc.arg(Action)::text
ORDER BY timestamp DESC LIMIT 1;

-- name: IsBatchActionPlanSaved :one
SELECT plan_saved FROM batch_action_run WHERE id = $1;

-- name: MarkBatchActionPlanSaved :execrows
UPDATE batch_action_run SET plan_saved = true WHERE id = $1 AND NOT plan_saved;

-- name: StartOverdueBatchPlan :one
UPDATE batch_action_run
SET overdue_cutoff = COALESCE(overdue_cutoff, date_trunc('second', now()))
WHERE id = $1 AND action_name = 'overdue'
RETURNING overdue_cutoff, overdue_cursor, plan_saved;

-- name: AdvanceOverdueBatchPlan :one
UPDATE batch_action_run
SET overdue_cursor = sqlc.arg(next_cursor),
    overdue_plan_size = overdue_plan_size + sqlc.arg(entry_count)::bigint,
    plan_saved = sqlc.arg(complete)::boolean
WHERE id = sqlc.arg(run_id) AND overdue_cursor = sqlc.arg(previous_cursor)
    AND overdue_cutoff IS NOT NULL AND NOT plan_saved
RETURNING overdue_plan_size;

-- name: InsertBatchActionPlanEntries :exec
INSERT INTO batch_action_plan (run_id, position, request_id, action_name, selection_error)
SELECT sqlc.arg(run_id), sqlc.arg(page_start)::bigint + ordinality, entry ->> 'requestId', entry ->> 'action', entry ->> 'error'
FROM jsonb_array_elements(sqlc.arg(entries)::jsonb) WITH ORDINALITY AS page(entry, ordinality);

-- name: GetBatchActionPlanEntries :many
SELECT position, request_id, action_name, selection_error
FROM batch_action_plan
WHERE run_id = sqlc.arg(run_id) AND position > sqlc.arg(after_position)
ORDER BY position
LIMIT sqlc.arg(page_size);

-- name: MarkBatchActionWorkStarted :execrows
UPDATE batch_action_run_event
SET work_started_at = now()
WHERE event_id = $1 AND work_started_at IS NULL;

-- name: GetBatchEmailDelivery :one
SELECT sqlc.embed(batch_action_email_delivery) FROM batch_action_email_delivery WHERE run_id = $1;

-- name: ListBatchRecoveryCandidates :many
SELECT link.event_id
FROM batch_action_run_event link
JOIN batch_action_run run ON run.id = link.run_id
JOIN event ON event.id = link.event_id
WHERE link.event_id > sqlc.arg(after_id) AND link.recovery_blocked_at IS NULL
  AND run.completed_at IS NULL AND event.event_status IN ('NEW', 'PROCESSING')
  AND (
    event.event_name = ANY(sqlc.arg(consumer_names)::varchar[])
    OR event.event_name = 'invoke-action'
  )
ORDER BY link.event_id
LIMIT sqlc.arg(page_size);

-- name: GetBatchRecoveryCandidate :one
SELECT sqlc.embed(event), link.run_id, link.attempt_id, link.attempt_lock_id, link.work_started_at,
       link.recovery_blocked_at, run.action_name
FROM event
JOIN batch_action_run_event link ON link.event_id = event.id
JOIN batch_action_run run ON run.id = link.run_id
WHERE event.id = $1 AND run.completed_at IS NULL;

-- name: LockBatchRecoveryCandidate :one
SELECT id FROM event WHERE id = $1 FOR UPDATE SKIP LOCKED;

-- name: TryBatchRecoveryLock :one
SELECT pg_try_advisory_xact_lock(hashtextextended('broker:batch-worker:' || sqlc.arg(lock_id)::text, 0));

-- name: ClearBatchActionAttempt :exec
UPDATE batch_action_run_event
SET attempt_id = NULL, attempt_claimed_at = NULL, attempt_lock_id = NULL
WHERE event_id = $1;

-- name: BlockBatchActionRecovery :exec
UPDATE batch_action_run_event
SET recovery_blocked_at = now(), recovery_reason = sqlc.arg(reason)
WHERE event_id = sqlc.arg(event_id);
