-- name: GetPatronRequestById :one
SELECT sqlc.embed(patron_request)
FROM patron_request
WHERE id = $1
LIMIT 1;

-- name: ListPatronRequests :many
SELECT sqlc.embed(patron_request)
FROM patron_request
ORDER BY timestamp;

-- name: SavePatronRequest :one
INSERT INTO patron_request (id, timestamp, ill_request, state, side, patron, requester_symbol, supplier_symbol, tenant)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE
    SET timestamp         = EXCLUDED.timestamp,
        ill_request       = EXCLUDED.ill_request,
        state             = EXCLUDED.state,
        side              = EXCLUDED.side,
        patron            = EXCLUDED.patron,
        requester_symbol  = EXCLUDED.requester_symbol,
        supplier_symbol   = EXCLUDED.supplier_symbol,
        tenant            = EXCLUDED.tenant
RETURNING sqlc.embed(patron_request);

-- name: DeletePatronRequest :exec
DELETE
FROM patron_request
WHERE id = $1;