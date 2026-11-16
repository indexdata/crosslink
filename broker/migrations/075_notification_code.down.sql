-- Older code only knows note and condition kinds and treats a non-null
-- condition as a loan condition. Reason rows keep their note text as plain
-- notes; those with no note would be empty messages, so they are removed.
DELETE FROM notification
WHERE kind IN ('cancellation_reason', 'unfilled_reason')
  AND (note IS NULL OR btrim(note) = '');

UPDATE notification
SET kind = 'note',
    code = NULL
WHERE kind IN ('cancellation_reason', 'unfilled_reason');

ALTER TABLE notification
    DROP COLUMN code_description;

ALTER TABLE notification
    RENAME COLUMN code TO condition;
