-- name: SavePullSlip :one
INSERT INTO pull_slip (id, created_at, generated_at, type, owner, search_criteria, pdf_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE
    SET generated_at    = EXCLUDED.generated_at,
        type            = EXCLUDED.type,
        owner           = EXCLUDED.owner,
        search_criteria = EXCLUDED.search_criteria,
        pdf_bytes       = EXCLUDED.pdf_bytes
RETURNING sqlc.embed(pull_slip);

-- name: GetPullSlipByIdAndOwner :one
SELECT sqlc.embed(pull_slip)
FROM pull_slip
WHERE id = $1 AND owner = $2
LIMIT 1;

