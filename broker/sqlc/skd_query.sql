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

-- name: ClaimNextScheduledTask :one
UPDATE scheduled_task
SET status     = 'running',
    updated_at = now()
WHERE id = (SELECT id
            FROM scheduled_task
            WHERE status = 'pending'
              AND run_at <= now()
            ORDER BY run_at
    LIMIT 1
    FOR
UPDATE SKIP LOCKED
    )
    RETURNING sqlc.embed(scheduled_task);

