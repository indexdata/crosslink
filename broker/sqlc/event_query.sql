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

-- name: ClaimEventForSignal :one
UPDATE event
SET signal = $2
WHERE id = (
    SELECT id FROM event
    WHERE signal != $2 AND event.id = $1
    LIMIT 1
)
RETURNING sqlc.embed(event);

-- name: GetIllTransactionEvents :many
SELECT sqlc.embed(event), COUNT(*) OVER () as full_count
FROM event
WHERE ill_transaction_id = $1
ORDER BY timestamp;

-- name: SaveEvent :one
INSERT INTO event (
    id, timestamp, ill_transaction_id, parent_id, event_type, event_name, event_status, event_data, result_data, signal
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
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
    signal = EXCLUDED.signal
RETURNING sqlc.embed(event);

-- name: DeleteEvent :exec
DELETE FROM event
WHERE id = $1;

-- name: DeleteEventsByIllTransaction :exec
DELETE FROM event
WHERE ill_transaction_id = $1;

-- name: UpdateEventStatus :exec
UPDATE event SET signal = '', event_status = $2
WHERE id = $1;
