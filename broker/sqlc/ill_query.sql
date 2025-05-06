-- name: GetPeerById :one
SELECT sqlc.embed(peer)
FROM peer
WHERE id = $1
LIMIT 1;

-- name: GetPeerBySymbol :one
SELECT sqlc.embed(peer)
FROM peer
         JOIN symbol ON peer_id = id
WHERE symbol_value = $1
LIMIT 1;

-- name: ListPeers :many
SELECT sqlc.embed(peer)
FROM peer
ORDER BY name
LIMIT $1 OFFSET $2;

-- name: SavePeer :one
INSERT INTO peer (id, name, refresh_policy, refresh_time, url, loans_count, borrows_count, vendor, custom_data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE
    SET name           = EXCLUDED.name,
        url            = EXCLUDED.url,
        refresh_policy = EXCLUDED.refresh_policy,
        refresh_time   = EXCLUDED.refresh_time,
        loans_count    = EXCLUDED.loans_count,
        borrows_count  = EXCLUDED.borrows_count,
        vendor         = EXCLUDED.vendor,
        custom_data    = EXCLUDED.custom_data
RETURNING sqlc.embed(peer);

-- name: DeletePeer :exec
DELETE
FROM peer
WHERE id = $1;

-- name: GetIllTransactionById :one
SELECT sqlc.embed(ill_transaction)
FROM ill_transaction
WHERE id = $1
LIMIT 1;

-- name: GetIllTransactionByRequesterId :many
SELECT sqlc.embed(ill_transaction)
FROM ill_transaction
WHERE requester_id = $1;

-- name: SaveSymbol :one
INSERT INTO symbol (symbol_value, peer_id)
VALUES ($1, $2)
RETURNING sqlc.embed(symbol);

-- name: GetSymbolsByPeerId :many
SELECT sqlc.embed(symbol)
FROM symbol
WHERE peer_id = $1;

-- name: DeleteSymbolByPeerId :exec
DELETE
FROM symbol
WHERE peer_id = $1;

-- name: GetIllTransactionByIdForUpdate :one
SELECT sqlc.embed(ill_transaction)
FROM ill_transaction
WHERE id = $1
    FOR UPDATE
LIMIT 1;

-- name: GetIllTransactionByRequesterRequestId :one
SELECT sqlc.embed(ill_transaction)
FROM ill_transaction
WHERE requester_request_id = $1
LIMIT 1;

-- name: GetIllTransactionByRequesterRequestIdForUpdate :one
SELECT sqlc.embed(ill_transaction)
FROM ill_transaction
WHERE requester_request_id = $1
    FOR UPDATE
LIMIT 1;

-- name: ListIllTransactions :many
SELECT sqlc.embed(ill_transaction)
FROM ill_transaction
ORDER BY timestamp
LIMIT $1 OFFSET $2;

-- name: SaveIllTransaction :one
INSERT INTO ill_transaction (id, timestamp, requester_symbol, requester_id, last_requester_action,
                             prev_requester_action, supplier_symbol, requester_request_id,
                             prev_requester_request_id, supplier_request_id,
                             last_supplier_status, prev_supplier_status, ill_transaction_data)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
ON CONFLICT (id) DO UPDATE
    SET timestamp                 = EXCLUDED.timestamp,
        requester_symbol          = EXCLUDED.requester_symbol,
        requester_id              = EXCLUDED.requester_id,
        last_requester_action     = EXCLUDED.last_requester_action,
        prev_requester_action     = EXCLUDED.prev_requester_action,
        supplier_symbol           = EXCLUDED.supplier_symbol,
        requester_request_id      = EXCLUDED.requester_request_id,
        prev_requester_request_id = EXCLUDED.prev_requester_request_id,
        supplier_request_id       = EXCLUDED.supplier_request_id,
        ill_transaction_data      = EXCLUDED.ill_transaction_data,
        last_supplier_status      = EXCLUDED.last_supplier_status,
        prev_supplier_status      = EXCLUDED.prev_supplier_status
RETURNING sqlc.embed(ill_transaction);

-- name: DeleteIllTransaction :exec
DELETE
FROM ill_transaction
WHERE id = $1;

-- name: GetLocatedSupplier :one
SELECT sqlc.embed(located_supplier)
FROM located_supplier
WHERE id = $1
LIMIT 1;

-- name: GetLocatedSupplierByIllTransition :many
SELECT sqlc.embed(located_supplier)
FROM located_supplier
WHERE ill_transaction_id = $1
ORDER BY ordinal;

-- name: ListLocatedSuppliers :many
SELECT sqlc.embed(located_supplier)
FROM located_supplier
ORDER BY ill_transaction_id, ordinal;

-- name: GetLocatedSupplierByIllTransactionAndStatus :many
SELECT sqlc.embed(located_supplier)
FROM located_supplier
WHERE ill_transaction_id = $1
  and supplier_status = $2;

-- name: GetLocatedSupplierByIllTransactionAndStatusForUpdate :many
SELECT sqlc.embed(located_supplier)
FROM located_supplier
WHERE ill_transaction_id = $1
  and supplier_status = $2
    FOR UPDATE;

-- name: GetLocatedSupplierByPeerId :many
SELECT sqlc.embed(located_supplier)
FROM located_supplier
WHERE supplier_id = $1;

-- name: GetLocatedSupplierByIllTransactionAndSupplierForUpdate :one
SELECT sqlc.embed(located_supplier)
FROM located_supplier
WHERE ill_transaction_id = $1
  and supplier_id = $2
    FOR UPDATE;

-- name: SaveLocatedSupplier :one
INSERT INTO located_supplier (id, ill_transaction_id, supplier_id, supplier_symbol, ordinal, supplier_status,
                              prev_action, prev_status,
                              last_action, last_status, local_id, prev_reason, last_reason, supplier_request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (id) DO UPDATE
    SET ill_transaction_id  = EXCLUDED.ill_transaction_id,
        supplier_id         = EXCLUDED.supplier_id,
        supplier_symbol     = EXCLUDED.supplier_symbol,
        ordinal             = EXCLUDED.ordinal,
        supplier_status     = EXCLUDED.supplier_status,
        prev_action         = EXCLUDED.prev_action,
        prev_status         = EXCLUDED.prev_status,
        last_action         = EXCLUDED.last_action,
        last_status         = EXCLUDED.last_status,
        local_id            = EXCLUDED.local_id,
        prev_reason         = EXCLUDED.prev_reason,
        last_reason         = EXCLUDED.last_reason,
        supplier_request_id = EXCLUDED.supplier_request_id
RETURNING sqlc.embed(located_supplier);

-- name: DeleteLocatedSupplier :exec
DELETE
FROM located_supplier
WHERE id = $1;

-- name: DeleteLocatedSupplierByIllTransaction :exec
DELETE
FROM located_supplier
WHERE ill_transaction_id = $1;
