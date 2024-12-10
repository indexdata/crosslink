-- name: EntryById :one
SELECT * FROM entries
WHERE id = $1 LIMIT 1;

-- name: ListEntries :many
SELECT sqlc.embed(e), sqlc.embed(s)
FROM entries e
LEFT JOIN entrysymbols s ON e.id = s.owner
ORDER BY e.name, e.id;

-- name: CreateEntry :one
INSERT INTO entries (
  name, contact_name, email_address
) VALUES (
  $1, $2, $3
)
RETURNING *;


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