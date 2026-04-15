ALTER TABLE notification
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'note';

UPDATE notification
SET kind = 'condition'
WHERE condition IS NOT NULL
   OR cost IS NOT NULL;
