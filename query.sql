-- name: EntryById :one
SELECT * FROM entries
WHERE id = $1 LIMIT 1;

-- name: ListEntries :many
SELECT sqlc.embed(e), sqlc.embed(s), a.symbol as symbol_authority
FROM entries e
LEFT JOIN entrysymbols s ON e.id = s.owner
LEFT JOIN authorities a ON a.id = s.authority
WHERE e.id = sqlc.narg(id) OR sqlc.narg(id) IS NULL
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


-- name: AuthorityBySymbol :one
SELECT * FROM authorities
WHERE symbol = $1 LIMIT 1;

-- name: CreateSymbol :one
INSERT INTO symbols (
  owner, symbol, authority
) VALUES (
  $1, $2, $3
)
RETURNING *;

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