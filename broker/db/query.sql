-- name: GetPeerById :one
SELECT sqlc.embed(peer) FROM peer
WHERE id = $1 LIMIT 1;

-- name: GetPeerBySymbol :one
SELECT sqlc.embed(peer) FROM peer
WHERE symbol = $1 LIMIT 1;

-- name: ListPeer :many
SELECT sqlc.embed(peer) FROM peer
ORDER BY name;

-- name: CreatePeer :one
INSERT INTO peer (
    id, symbol, name, address
) VALUES (
             $1, $2, $3, $4
         )
RETURNING sqlc.embed(peer);

-- name: DeletePeer :exec
DELETE FROM peer
WHERE id = $1;

-- name: GetIllTransaction :one
SELECT sqlc.embed(ill_transaction) FROM ill_transaction
WHERE id = $1 LIMIT 1;

-- name: ListIllTransaction :many
SELECT sqlc.embed(ill_transaction) FROM ill_transaction
ORDER BY timestamp;

-- name: CreateIllTransaction :one
INSERT INTO ill_transaction (
    id, timestamp, requester_symbol, requester_id, requester_action, supplier_symbol, state, requester_request_id, supplier_request_id, ill_transaction_data
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
         )
RETURNING sqlc.embed(ill_transaction);

-- name: DeleteIllTransaction :exec
DELETE FROM ill_transaction
WHERE id = $1;

-- name: GetEventType :one
SELECT sqlc.embed(event_type) FROM event_type
WHERE type = $1 LIMIT 1;

-- name: ListEventType :many
SELECT sqlc.embed(event_type) FROM event_type
ORDER BY type;

-- name: CreateEventType :one
INSERT INTO event_type (
    type, retry_count
) VALUES (
             $1, $2
         )
RETURNING sqlc.embed(event_type);

-- name: DeleteEventType :exec
DELETE FROM event_type
WHERE type = $1;


-- name: GetEvent :one
SELECT sqlc.embed(event) FROM event
WHERE id = $1 LIMIT 1;

-- name: ListEvent :many
SELECT sqlc.embed(event) FROM event
ORDER BY created_at;

-- name: CreateEvent :one
INSERT INTO event (
    id, ill_transaction_id, event_type, event_status, event_data, result_data
) VALUES (
             $1, $2, $3, $4, $5, $6
         )
RETURNING sqlc.embed(event);

-- name: DeleteEvent :exec
DELETE FROM event
WHERE id = $1;

