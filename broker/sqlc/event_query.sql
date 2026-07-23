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

-- name: DeleteEventsByIllTransaction :exec
DELETE FROM event
WHERE ill_transaction_id = $1;

-- name: HasActiveBatchActionEvents :one
SELECT EXISTS (
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

-- name: UpdateEventLifecycle :one
UPDATE event SET last_signal = $3, event_status = $2
WHERE id = $1
RETURNING sqlc.embed(event);

-- name: GetLatestRequestEventByAction :one
SELECT sqlc.embed(event) FROM event
WHERE ill_transaction_id = sqlc.arg(IllTransactionID) AND event_name = 'requester-msg-received' AND
    (event_data -> 'incomingMessage' -> 'requestingAgencyMessage' ->> 'action')::text = sqlc.arg(Action)::text
ORDER BY timestamp DESC LIMIT 1;
