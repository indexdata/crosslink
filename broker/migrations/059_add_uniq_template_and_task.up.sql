CREATE OR REPLACE FUNCTION check_template_owner_labels_unique()
    RETURNS trigger AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM template t
        WHERE t.owner = NEW.owner
          AND t.labels && NEW.labels
          AND (TG_OP = 'INSERT' OR t.id <> NEW.id)
    ) THEN
        RAISE EXCEPTION
            'One or more labels already exist for owner %',
            NEW.owner;
END IF;
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER  trg_check_template_owner_labels_unique
    BEFORE INSERT OR UPDATE OF owner, labels
                     ON template
                         FOR EACH ROW
                         EXECUTE FUNCTION check_template_owner_labels_unique();

-- Remove duplicates if already exist
WITH duplicates AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY owner, title
            ORDER BY id
        ) AS rn
    FROM scheduled_task
)
UPDATE scheduled_task st
SET title = st.title || '_' || d.rn
    FROM duplicates d
WHERE st.id = d.id
  AND d.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduled_task_owner_title
    ON scheduled_task (owner, title);