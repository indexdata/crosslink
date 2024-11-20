-- name: GetEntry :one
SELECT * FROM directory_entries
WHERE id = $1 LIMIT 1;

-- name: ListEntries :many
SELECT * FROM directory_entries
ORDER BY name;