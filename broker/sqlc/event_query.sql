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

-- name: LockBatchActionDispatch :exec
SELECT pg_advisory_xact_lock(hashtextextended('crosslink.batch:' || sqlc.arg(action_name)::text, 0));

-- name: GetConflictingBatchActionRun :one
SELECT batch_action_run.id, event.timestamp AS started_at
FROM batch_action_run
JOIN event ON event.id = batch_action_run.id
WHERE action_name = sqlc.arg(action_name)::text AND completed_at IS NULL
  AND (owner = sqlc.arg(owner)::text OR owner = '' OR sqlc.arg(owner)::text = '')
ORDER BY event.timestamp, batch_action_run.id
LIMIT 1;

-- name: AddBatchActionRun :exec
INSERT INTO batch_action_run (id, scheduled_task_id, owner, action_name)
VALUES ($1, $2, $3, $4);

-- name: AddBatchActionRunEvent :exec
INSERT INTO batch_action_run_event (event_id, run_id, worker_event_id, ready_at)
VALUES ($1, $2, $3, CASE WHEN $1::varchar = $3::varchar THEN clock_timestamp() ELSE NULL END);

-- name: GetBatchEventWorker :one
SELECT worker_event_id FROM batch_action_run_event WHERE event_id = $1;

-- name: LockBatchRunForEvent :one
SELECT sqlc.embed(batch_action_run)
FROM batch_action_run_event
JOIN batch_action_run ON batch_action_run.id = batch_action_run_event.run_id
WHERE batch_action_run_event.event_id = $1
FOR UPDATE OF batch_action_run;

-- name: NextBatchWorkerEvent :one
SELECT event_id FROM batch_action_run_event
JOIN event ON event.id = event_id
WHERE ready_at IS NOT NULL AND event_status = 'NEW'
  AND event_name = ANY(sqlc.arg(event_names)::varchar[])
ORDER BY ready_at, event_id
FOR UPDATE OF batch_action_run_event SKIP LOCKED
LIMIT 1;

-- name: StartBatchWorkerLease :one
UPDATE batch_action_run_event
SET ready_at = NULL,
    lease_expires_at = clock_timestamp() + LEAST(sqlc.arg(lease_duration)::interval, sqlc.arg(lease_cap)::interval),
    lease_cap_at = clock_timestamp() + sqlc.arg(lease_cap)::interval
WHERE event_id = sqlc.arg(event_id)::text AND ready_at IS NOT NULL
RETURNING lease_expires_at, clock_timestamp()::timestamptz AS database_now;

-- name: BeginBatchWorkerEvent :one
UPDATE event
SET event_status = 'PROCESSING', last_signal = 'task_begin'
WHERE id = $1 AND event_status = 'NEW'
RETURNING sqlc.embed(event);

-- name: RenewBatchWorkerLease :one
UPDATE batch_action_run_event
SET lease_expires_at = LEAST(clock_timestamp() + sqlc.arg(lease_duration)::interval, lease_cap_at)
WHERE event_id = sqlc.arg(event_id)::text AND NOT finished AND lease_expires_at > clock_timestamp()
RETURNING lease_expires_at, clock_timestamp()::timestamptz AS database_now;

-- name: GetActiveBatchWorkerLease :one
SELECT lease_expires_at, clock_timestamp()::timestamptz AS database_now
FROM batch_action_run_event
WHERE event_id = $1 AND lease_expires_at > clock_timestamp();

-- name: GetInterruptedBatchWorkers :many
(
    SELECT event_id AS worker_id, run_id FROM batch_action_run_event
    WHERE lease_expires_at <= clock_timestamp()
    ORDER BY lease_expires_at, event_id
    LIMIT sqlc.arg(batch_limit)::int
)
UNION ALL
(
    -- A patron request deletion can cascade to a worker event before its
    -- delegated protocol tasks (which live in the ILL transaction domain).
    SELECT DISTINCT child.worker_event_id AS worker_id, child.run_id
    FROM batch_action_run_event child
    WHERE NOT child.finished AND child.worker_event_id <> child.event_id
      AND NOT EXISTS (
        SELECT 1 FROM batch_action_run_event worker WHERE worker.event_id = child.worker_event_id
      )
    LIMIT sqlc.arg(batch_limit)::int
);

-- name: LockExpiredBatchWorker :one
SELECT event_id FROM batch_action_run_event
WHERE event_id = $1 AND lease_expires_at <= clock_timestamp()
FOR UPDATE SKIP LOCKED;

-- name: FinishAbandonedWorkerEvents :exec
-- Recovery clears pending signals without invoking protocol completion handlers.
WITH abandoned AS (
    UPDATE batch_action_run_event
    SET finished = TRUE, ready_at = NULL
    WHERE worker_event_id = $1 AND NOT finished
    RETURNING event_id
)
UPDATE event
SET event_status = 'ERROR', last_signal = '',
    result_data = '{"eventError":{"Message":"batch worker interrupted","Cause":"The worker did not complete this task."}}'
FROM abandoned
WHERE event.id = abandoned.event_id AND event.event_status IN ('NEW', 'PROCESSING');

-- name: FinishBatchRunEvent :exec
UPDATE batch_action_run_event
SET finished = TRUE, ready_at = NULL
WHERE event_id = $1;

-- name: ClearIdleBatchWorkerLease :exec
UPDATE batch_action_run_event AS worker
SET lease_expires_at = NULL
WHERE worker.event_id = $1 AND worker.lease_expires_at IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM batch_action_run_event
    WHERE worker_event_id = worker.event_id AND NOT finished
  );

-- name: ListIdleBatchRuns :many
SELECT id FROM batch_action_run
WHERE completed_at IS NULL AND NOT EXISTS (
    SELECT 1 FROM batch_action_run_event WHERE run_id = batch_action_run.id AND NOT finished
)
ORDER BY id LIMIT $1;

-- name: LockBatchRun :one
SELECT sqlc.embed(batch_action_run) FROM batch_action_run
WHERE id = $1 AND completed_at IS NULL FOR UPDATE SKIP LOCKED;

-- name: IsBatchRunIdle :one
SELECT NOT EXISTS (SELECT 1 FROM batch_action_run_event WHERE run_id = $1 AND NOT finished);

-- name: FinishBatchRun :exec
UPDATE batch_action_run SET completed_at = clock_timestamp() WHERE id = $1;

-- name: ExpireMessageConfirmationTasks :exec
WITH expired AS (
    SELECT event.id
    FROM message_confirmation_expiry AS expiry
    JOIN event ON event.id = expiry.event_id
    WHERE expiry.expires_at <= statement_timestamp()
        AND event.event_status IN ('NEW', 'PROCESSING')
    ORDER BY expiry.expires_at, expiry.event_id
    LIMIT sqlc.arg(batch_limit)::int
    FOR UPDATE OF event SKIP LOCKED
)
UPDATE event
SET event_status = 'ERROR', last_signal = '',
    result_data = '{"eventError":{"Message":"ISO 18626 confirmation expired","Cause":"The request waiter could no longer receive this confirmation."}}'
FROM expired
WHERE event.id = expired.id;

-- name: UpdateEventLifecycle :one
UPDATE event SET last_signal = $3, event_status = $2
WHERE id = $1
RETURNING sqlc.embed(event);

-- name: GetLatestRequestEventByAction :one
SELECT sqlc.embed(event) FROM event
WHERE ill_transaction_id = sqlc.arg(IllTransactionID) AND event_name = 'requester-msg-received' AND
    (event_data -> 'incomingMessage' -> 'requestingAgencyMessage' ->> 'action')::text = sqlc.arg(Action)::text
ORDER BY timestamp DESC LIMIT 1;
