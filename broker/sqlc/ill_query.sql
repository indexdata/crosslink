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

-- name: GetLocatedSupplier :one
SELECT sqlc.embed(located_supplier) FROM located_supplier
WHERE id = $1 LIMIT 1;

-- name: GetLocatedSupplierByIllTransition :many
SELECT sqlc.embed(located_supplier) FROM located_supplier
WHERE ill_transaction_id = $1
ORDER BY ordinal;

-- name: GetLocatedSupplierByIllTransactionAndStatus :many
SELECT sqlc.embed(located_supplier) FROM located_supplier
WHERE ill_transaction_id = $1 and supplier_status = $2;

-- name: GetLocatedSupplierByIllTransactionAndSupplier :one
SELECT sqlc.embed(located_supplier) FROM located_supplier
WHERE ill_transaction_id = $1 and supplier_id = $2;

-- name: SaveLocatedSupplier :one
INSERT INTO located_supplier (
    id, ill_transaction_id, supplier_id, ordinal, supplier_status, previous_action, previous_status, last_action, last_status
) VALUES (
             $1, $2, $3, $4, $5, $6, $7, $8, $9
         )
ON CONFLICT (id) DO UPDATE
    SET ill_transaction_id = EXCLUDED.ill_transaction_id,
        supplier_id = EXCLUDED.supplier_id,
        ordinal = EXCLUDED.ordinal,
        supplier_status = EXCLUDED.supplier_status,
        previous_action = EXCLUDED.previous_action,
        previous_status = EXCLUDED.previous_status,
        last_action = EXCLUDED.last_action,
        last_status = EXCLUDED.last_status
RETURNING sqlc.embed(located_supplier);

-- name: DeleteLocatedSupplier :exec
DELETE FROM located_supplier
WHERE id = $1;
