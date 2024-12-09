-- name: GetPeerById :one
SELECT * FROM peer
WHERE id = $1 LIMIT 1;

-- name: GetPeerBySymbol :one
SELECT * FROM peer
WHERE symbol = $1 LIMIT 1;

-- name: ListPeer :many
SELECT * FROM peer
ORDER BY name;

-- name: CreatePeer :one
INSERT INTO peer (
    id, symbol, name, address
) VALUES (
             $1, $2, $3, $4
         )
RETURNING *;

-- name: DeletePeer :exec
DELETE FROM peer
WHERE id = $1;

-- name: GetTransaction :one
SELECT * FROM transaction
WHERE id = $1 LIMIT 1;

-- name: ListTransaction :many
SELECT * FROM transaction
ORDER BY timestamp;

-- name: CreateTransaction :one
INSERT INTO transaction (
    id, timestamp, requester_symbol, requester_id, requester_action, supplier_symbol, state, requester_request_id, supplier_request_id, data
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
         )
RETURNING *;

-- name: DeleteTransaction :exec
DELETE FROM transaction
WHERE id = $1;

-- name: GetEventType :one
SELECT * FROM event_type
WHERE type = $1 LIMIT 1;

-- name: ListEventType :many
SELECT * FROM event_type
ORDER BY type;

-- name: CreateEventType :one
INSERT INTO event_type (
    type, retry_count
) VALUES (
             $1, $2
         )
RETURNING *;

-- name: DeleteEventType :exec
DELETE FROM event_type
WHERE type = $1;


-- name: GetEvent :one
SELECT * FROM event
WHERE id = $1 LIMIT 1;

-- name: ListEvent :many
SELECT * FROM event
ORDER BY created_at;

-- name: CreateEvent :one
INSERT INTO event (
    id, transaction_id, event_type, event_status, event_data, result_data
) VALUES (
             $1, $2, $3, $4, $5, $6
         )
RETURNING *;

-- name: DeleteEvent :exec
DELETE FROM event
WHERE id = $1;