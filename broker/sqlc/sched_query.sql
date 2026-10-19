-- name: SaveScheduledTask :one
INSERT INTO scheduled_task (id, event_name, schedule, action_data, title, run_at, status, owner, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) ON CONFLICT (id) DO
UPDATE
    SET event_name = EXCLUDED.event_name,
    schedule = EXCLUDED.schedule,
    action_data = EXCLUDED.action_data,
    title = EXCLUDED.title,
    run_at = EXCLUDED.run_at,
    status = EXCLUDED.status,
    owner = EXCLUDED.owner,
    updated_at = now()
    RETURNING sqlc.embed(scheduled_task);

-- name: GetNextRunAt :one
SELECT run_at
FROM scheduled_task
WHERE status = 'pending'
  AND run_at IS NOT NULL
ORDER BY run_at LIMIT 1;

-- name: GetStuckRunningTasks :many
SELECT sqlc.embed(scheduled_task)
FROM scheduled_task
WHERE status = 'running'
  AND updated_at <= now() - $1::interval;

-- name: ClaimNextScheduledTask :one
UPDATE scheduled_task
SET status     = 'running',
    updated_at = now()
WHERE id = (SELECT id
            FROM scheduled_task
            WHERE status = 'pending'
              AND run_at <= now()
              AND run_at IS NOT NULL
            ORDER BY run_at
    LIMIT 1
    FOR
UPDATE SKIP LOCKED
    )
    RETURNING sqlc.embed(scheduled_task);

-- name: GetScheduledTaskById :one
SELECT sqlc.embed(scheduled_task)
FROM scheduled_task
WHERE id = sqlc.arg(id)
  AND (sqlc.arg(owners)::text[] IS NULL OR owner = ANY(sqlc.arg(owners)::text[]));

-- name: GetScheduledTaskByIdForUpdate :one
SELECT sqlc.embed(scheduled_task)
FROM scheduled_task
WHERE id = sqlc.arg(id)
  AND (sqlc.arg(owners)::text[] IS NULL OR owner = ANY(sqlc.arg(owners)::text[]))
FOR UPDATE;

-- name: GetScheduledTasks :many
SELECT sqlc.embed(scheduled_task), COUNT(*) OVER () as full_count
FROM scheduled_task
WHERE sqlc.arg(owners)::text[] IS NULL OR owner = ANY(sqlc.arg(owners)::text[])
ORDER BY created_at LIMIT $1
OFFSET $2;

-- name: DeleteScheduledTask :exec
DELETE
FROM scheduled_task
WHERE id = sqlc.arg(id)
  AND (sqlc.arg(owners)::text[] IS NULL OR owner = ANY(sqlc.arg(owners)::text[]));
