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

-- name: GetIllTransactionById :one
SELECT sqlc.embed(ill_transaction) FROM ill_transaction
WHERE id = $1 LIMIT 1;

-- name: GetIllTransactionByRequesterRequestId :one
SELECT sqlc.embed(ill_transaction) FROM ill_transaction
WHERE requester_request_id = $1 LIMIT 1;

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

-- name: GetEventConfig :one
SELECT sqlc.embed(event_config) FROM event_config
WHERE event_name = $1 LIMIT 1;

-- name: ListEventConfig :many
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

-- name: ListEvent :many
SELECT sqlc.embed(event) FROM event
ORDER BY timestamp;

-- name: CreateEvent :one
INSERT INTO event (
    id, ill_transaction_id, timestamp, event_name, event_type, event_status, event_data, result_data
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8
         )
RETURNING sqlc.embed(event);

-- name: DeleteEvent :exec
DELETE FROM event
WHERE id = $1;

-- name: GetLocatedSupplier :one
SELECT sqlc.embed(located_supplier) FROM located_supplier
WHERE id = $1 LIMIT 1;

-- name: GetLocatedSupplierByIllTransition :many
SELECT sqlc.embed(located_supplier) FROM located_supplier
WHERE ill_transaction_id = $1
ORDER BY ordinal;

-- name: CreateLocatedSupplier :one
INSERT INTO located_supplier (
    id, ill_transaction_id, supplier_id, ordinal, supplier_status
) VALUES (
             $1, $2, $3, $4, $5
         )
RETURNING sqlc.embed(located_supplier);

-- name: DeleteLocatedSupplier :exec
DELETE FROM located_supplier
WHERE id = $1;

