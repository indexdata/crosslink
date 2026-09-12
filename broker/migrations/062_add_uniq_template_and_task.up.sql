-- Prevent template writes from introducing an overlap between this preflight
-- and installation of the row-level trigger below.
LOCK TABLE template IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    conflicts TEXT;
BEGIN
    WITH overlapping_labels AS (
        SELECT t.owner, label_value.label
        FROM template t
        CROSS JOIN LATERAL unnest(t.labels) AS label_value(label)
        WHERE label_value.label IS NOT NULL
        GROUP BY t.owner, label_value.label
        HAVING COUNT(DISTINCT t.id) > 1
    ), overlaps_by_owner AS (
        SELECT owner, ARRAY_AGG(label ORDER BY label) AS labels
        FROM overlapping_labels
        GROUP BY owner
    )
    SELECT STRING_AGG(
        FORMAT('owner=%L labels=%s', owner, labels::TEXT),
        '; ' ORDER BY owner
    )
    INTO conflicts
    FROM overlaps_by_owner;

    IF conflicts IS NOT NULL THEN
        RAISE EXCEPTION 'Template label overlaps already exist: %', conflicts
            USING ERRCODE = 'unique_violation';
    END IF;
END;
$$;

CREATE OR REPLACE FUNCTION check_template_owner_labels_unique()
    RETURNS trigger AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM template t
        WHERE t.owner = NEW.owner
          AND t.labels && NEW.labels
          AND t.id <> NEW.id
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
