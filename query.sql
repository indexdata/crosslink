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
  name, contact_name, email
) VALUES (
  $1, $2, $3
)
RETURNING *;

-- name: UpdateEntry :exec
UPDATE entries
SET
  name = coalesce(sqlc.narg(name), CASE WHEN NOT sqlc.arg(del_name)::bool THEN name END),
  contact_name = coalesce(sqlc.narg(contact_name), CASE WHEN NOT sqlc.arg(del_contact_name)::bool THEN contact_name END),
  email = coalesce(sqlc.narg(email), CASE WHEN NOT sqlc.arg(del_email)::bool THEN email END)
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