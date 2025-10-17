-- name: EntryById :one
SELECT * FROM entries WHERE id = $1 LIMIT 1;

-- name: EntryBySymbol :one
SELECT e.* FROM entries e, symbols s WHERE e.id = s.owner AND s.authority = @authority AND s.symbol = @symbol LIMIT 1;

-- name: CreateEntry :one
INSERT INTO entries (
  name, contact_name, email
) VALUES (
  $1, $2, $3
)
RETURNING *;

-- name: UpdateEntry :exec
UPDATE entries
SET
  name = @name,
  contact_name = @contact_name,
  email = @email
WHERE id = @id;

-- name: DeleteEntryById :exec
DELETE from entries WHERE id = @id;

-- name: DeleteEntryBySymbol :exec
DELETE from entries USING symbols
WHERE entries.id = symbols.owner
  AND symbols.authority = @authority
  AND symbols.symbol = @symbol;

-- name: UpsertSymbol :one
INSERT INTO symbols (
  id, owner, symbol, authority
) VALUES (
  coalesce(sqlc.narg('id'), gen_random_uuid()),
  @owner,
  @symbol,
  @authority
)
ON CONFLICT (id) DO UPDATE SET
  owner = @owner,
  symbol = @symbol,
  authority = @authority
WHERE symbols.id = sqlc.narg('id')
RETURNING *;

-- name: DeleteOtherOwnedSymbols :exec
DELETE FROM symbols WHERE owner = @owner AND ID <> ALL(@ids::uuid[]);

-- name: DeleteAllOwnedSymbols :exec
DELETE FROM symbols WHERE owner = @owner;


-- name: UpsertServiceEndpoint :one
INSERT INTO service_endpoints (
  id, entry, name, type, address
) VALUES (
  coalesce(sqlc.narg('id'), gen_random_uuid()),
  @entry,
  @name,
  @type,
  @address
)
ON CONFLICT (id) DO UPDATE SET
  entry = @entry,
  name = @name,
  type = @type,
  address = @address
WHERE service_endpoints.id = sqlc.narg('id')
RETURNING *;

-- name: DeleteOtherOwnedServiceEndpoints :exec
DELETE FROM service_endpoints WHERE entry = @entry AND ID <> ALL(@ids::uuid[]);

-- name: DeleteAllOwnedServiceEndpoints :exec
DELETE FROM service_endpoints WHERE entry = @entry;


-- name: ListConsortia :many
SELECT * FROM consortia
WHERE
  (id = sqlc.narg(id) OR sqlc.narg(id) IS NULL);

-- name: ConsortiumById :one
SELECT * FROM consortia
WHERE id = $1 LIMIT 1;

-- name: CreateConsortium :one
INSERT INTO consortia (
  name, entry
) VALUES (
  @name, @entry
)
RETURNING *;

-- name: UpdateConsortium :exec
UPDATE consortia
SET
  name = @name,
  entry = @entry
WHERE id = @id;

-- name: DeleteConsortium :exec
DELETE from consortia where id = @id;
