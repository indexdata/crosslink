-- name: GetDirectoryEntry :one
SELECT * FROM directory_entries
WHERE id = $1 LIMIT 1;

-- name: ListDirectoryEntries :many
SELECT * FROM directory_entries
ORDER BY name;