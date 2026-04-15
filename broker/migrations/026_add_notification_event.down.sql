DELETE FROM event_config WHERE event_name = 'send-notification';

ALTER TABLE notification DROP COLUMN direction,
                        ADD COLUMN side TEXT NOT NULL DEFAULT 'borrowing';

UPDATE notification
SET side = pr.side
    FROM patron_request pr
WHERE pr.id = notification.pr_id;