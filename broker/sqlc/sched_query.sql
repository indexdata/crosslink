-- name: SaveScheduledTask :one
INSERT INTO scheduled_task (id, event_name, cron_expr, payload, run_at, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE
    SET event_name = EXCLUDED.event_name,
        cron_expr  = EXCLUDED.cron_expr,
        payload    = EXCLUDED.payload,
        run_at     = EXCLUDED.run_at,
        status     = EXCLUDED.status,
        updated_at = now()
RETURNING sqlc.embed(scheduled_task);

-- name: GetNextRunAt :one
SELECT run_at
FROM scheduled_task
WHERE status = 'pending'
  AND run_at IS NOT NULL
ORDER BY run_at
LIMIT 1;

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
              AND run_at <= now() AND run_at IS NOT NULL
            ORDER BY run_at
    LIMIT 1
    FOR
UPDATE SKIP LOCKED
    )
    RETURNING sqlc.embed(scheduled_task);

-- name: GetScheduledTaskById :one
SELECT sqlc.embed(scheduled_task)
FROM scheduled_task
WHERE id = $1;

-- name: SaveBatchAction :one
INSERT INTO batch_action (id, action_name, schedule, batch_query, owner, scheduled_task_id, action_params, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (id) DO UPDATE
    SET action_name       = EXCLUDED.action_name,
        schedule          = EXCLUDED.schedule,
        batch_query       = EXCLUDED.batch_query,
        owner             = EXCLUDED.owner,
        scheduled_task_id = EXCLUDED.scheduled_task_id,
        action_params = EXCLUDED.action_params,
        updated_at        = now()
RETURNING sqlc.embed(batch_action);

-- name: GetBatchActionById :one
SELECT sqlc.embed(batch_action)
FROM batch_action
WHERE id = $1 AND owner = $2;

-- name: GetBatchActions :many
SELECT sqlc.embed(batch_action), COUNT(*) OVER () as full_count
FROM batch_action
WHERE owner = $3
ORDER BY created_at
LIMIT $1 OFFSET $2;

-- name: DeleteBatchAction :exec
DELETE FROM batch_action
WHERE id = $1 AND owner = $2;

