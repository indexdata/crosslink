-- name: EntryById :one
SELECT * FROM entries
WHERE id = $1 LIMIT 1;

-- name: ListEntries :many
SELECT sqlc.embed(e), sqlc.embed(s), a.symbol as symbol_authority, sqlc.embed(ep)
FROM entries e
LEFT JOIN entrysymbols s ON e.id = s.owner
LEFT JOIN authorities a ON a.id = s.authority
LEFT JOIN entryendpoints ep ON e.id = ep.entry
WHERE
  (e.id = sqlc.narg(id) OR sqlc.narg(id) IS NULL)
  AND (
    (e.id = (
      SELECT owner FROM symbols s2, authorities a2 WHERE a2.symbol = sqlc.narg(authority) AND s2.symbol = sqlc.narg(symbol)
    ))
    OR (sqlc.narg(authority) IS NULL AND sqlc.narg(symbol) IS NULL)
  )
ORDER BY e.name, e.id;

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

-- name: DeleteEntry :exec
DELETE from entries where id = @id;


-- name: AuthorityBySymbol :one
SELECT * FROM authorities
WHERE symbol = $1 LIMIT 1;

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


-- name: ListAuthorities :many
SELECT * FROM authorities;

-- name: CreateAuthority :one
INSERT INTO authorities (
  symbol
) VALUES (
  @symbol
)
RETURNING *;


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