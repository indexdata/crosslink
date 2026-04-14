INSERT INTO event_config (event_name, event_type, retry_count)
VALUES ('send-notification', 'TASK', 1);

ALTER TABLE notification DROP COLUMN side,
                        ADD COLUMN direction TEXT NOT NULL DEFAULT 'sent';

UPDATE notification
SET direction = 'received'
    FROM patron_request pr
WHERE pr.id = notification.pr_id
  AND pr.side = 'borrowing'
  AND pr.requester_symbol = notification.to_symbol;

UPDATE notification
SET direction = 'received'
    FROM patron_request pr
WHERE pr.id = notification.pr_id
  AND pr.side = 'lending'
  AND pr.supplier_symbol = notification.to_symbol;