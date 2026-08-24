-- name: LockImportPatronRequest :one
SELECT id, requester_req_id
FROM patron_request
WHERE id = $1
FOR UPDATE;

-- name: UpdateImportedPatronRequest :exec
UPDATE patron_request
SET created_at = sqlc.arg(created_at),
    ill_request = sqlc.arg(ill_request),
    state = sqlc.arg(state),
    side = sqlc.arg(side),
    patron = sqlc.arg(patron),
    requester_symbol = sqlc.arg(requester_symbol),
    supplier_symbol = sqlc.arg(supplier_symbol),
    tenant = sqlc.arg(tenant),
    requester_req_id = sqlc.arg(requester_req_id),
    needs_attention = sqlc.arg(needs_attention),
    last_action = sqlc.arg(last_action),
    last_action_outcome = sqlc.arg(last_action_outcome),
    last_action_result = sqlc.arg(last_action_result),
    items = sqlc.arg(items),
    language = sqlc.arg(language),
    terminal_state = sqlc.arg(terminal_state),
    updated_at = sqlc.arg(updated_at),
    ill_response = sqlc.arg(ill_response),
    internal_note = sqlc.arg(internal_note),
    next_req_id = sqlc.arg(next_req_id),
    prev_req_id = sqlc.arg(prev_req_id),
    retry_bib_info = sqlc.arg(retry_bib_info),
    state_model = sqlc.arg(state_model)
WHERE id = sqlc.arg(id);

-- name: DeleteImportedItemsNotPresent :exec
DELETE FROM item
WHERE pr_id = sqlc.arg(pr_id)
  AND id <> ALL(sqlc.arg(ids)::text[]);

-- name: DeleteImportedNotificationsNotPresent :exec
DELETE FROM notification
WHERE pr_id = sqlc.arg(pr_id)
  AND id <> ALL(sqlc.arg(ids)::text[]);

-- name: DeleteImportedLocatedSuppliersNotPresent :exec
DELETE FROM located_supplier
WHERE ill_transaction_id = sqlc.arg(ill_transaction_id)
  AND id <> ALL(sqlc.arg(ids)::text[]);

-- name: GetImportItemParent :one
SELECT pr_id FROM item WHERE id = $1;

-- name: GetImportNotificationParent :one
SELECT pr_id FROM notification WHERE id = $1;

-- name: GetImportLocatedSupplierParent :one
SELECT ill_transaction_id FROM located_supplier WHERE id = $1;

-- name: LockImportIllTransaction :one
SELECT id, requester_request_id
FROM ill_transaction
WHERE id = $1
FOR UPDATE;

-- name: GetImportIllTransactionByRequesterRequestID :one
SELECT id, requester_request_id
FROM ill_transaction
WHERE requester_request_id = $1;

-- name: LockImportTemplatesByLabels :many
SELECT id, created_at
FROM template
WHERE owner = sqlc.arg(owner)
  AND labels && sqlc.arg(labels)::text[]
ORDER BY id
FOR UPDATE;

-- name: LockImportBatchAction :one
SELECT id, created_at
FROM scheduled_task
WHERE owner = sqlc.arg(owner)
  AND title = sqlc.arg(title)
FOR UPDATE;
