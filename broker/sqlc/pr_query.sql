-- name: GetPatronRequestById :one
SELECT sqlc.embed(patron_request)
FROM patron_request
WHERE id = $1
LIMIT 1;

-- name: ListPatronRequests :many
SELECT sqlc.embed(patron_request), COUNT(*) OVER () as full_count
FROM patron_request
ORDER BY timestamp
LIMIT $1 OFFSET $2;

-- name: SavePatronRequest :one
INSERT INTO patron_request (id, timestamp, ill_request, state, side, patron, requester_symbol, supplier_symbol, tenant, requester_req_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO UPDATE
    SET timestamp         = EXCLUDED.timestamp,
        ill_request       = EXCLUDED.ill_request,
        state             = EXCLUDED.state,
        side              = EXCLUDED.side,
        patron            = EXCLUDED.patron,
        requester_symbol  = EXCLUDED.requester_symbol,
        supplier_symbol   = EXCLUDED.supplier_symbol,
        tenant            = EXCLUDED.tenant,
        requester_req_id  = EXCLUDED.requester_req_id
RETURNING sqlc.embed(patron_request);

-- name: DeletePatronRequest :exec
DELETE
FROM patron_request
WHERE id = $1;

-- name: GetPatronRequestBySupplierSymbolAndRequesterReqId :one
-- params: supplier_symbol string, requester_req_id string
SELECT sqlc.embed(patron_request)
FROM patron_request
WHERE supplier_symbol = $1 AND requester_req_id = $2
LIMIT 1;

-- name: GetNextHrid :one
SELECT get_next_hrid($1)::TEXT AS hrid;

-- name: SaveItem :one
INSERT INTO item (id, pr_id, barcode, call_number, title, item_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE
    SET pr_id       = EXCLUDED.pr_id,
        barcode = EXCLUDED.barcode,
        call_number = EXCLUDED.call_number,
        title = EXCLUDED.title,
        item_id = EXCLUDED.item_id,
        created_at = EXCLUDED.created_at
RETURNING sqlc.embed(item);

-- name: GetItemById :one
SELECT sqlc.embed(item)
FROM item
WHERE id = $1
LIMIT 1;

-- name: GetItemsByPrId :many
SELECT sqlc.embed(item)
FROM item
WHERE pr_id = $1;

-- name: DeleteItemById :exec
DELETE
FROM item
WHERE id = $1;

-- name: SaveNotification :one
INSERT INTO notification (id, pr_id, from_symbol, to_symbol, side, note, cost, currency, condition, receipt, created_at, acknowledged_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (id) DO UPDATE
    SET pr_id           = EXCLUDED.pr_id,
        from_symbol     = EXCLUDED.from_symbol,
        to_symbol       = EXCLUDED.to_symbol,
        side            = EXCLUDED.side,
        note            = EXCLUDED.note,
        cost            = EXCLUDED.cost,
        currency        = EXCLUDED.currency,
        condition       = EXCLUDED.condition,
        receipt         = EXCLUDED.receipt,
        created_at      = EXCLUDED.created_at,
        acknowledged_at = EXCLUDED.acknowledged_at
RETURNING sqlc.embed(notification);

-- name: GetNotificationById :one
SELECT sqlc.embed(notification)
FROM notification
WHERE id = $1
LIMIT 1;

-- name: GetNotificationsByPrId :many
SELECT sqlc.embed(notification)
FROM notification
WHERE pr_id = $1;

-- name: DeleteNotificationById :exec
DELETE
FROM notification
WHERE id = $1;