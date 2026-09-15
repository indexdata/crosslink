-- Prevent template writes from introducing an overlap between this preflight
-- and installation of the row-level trigger below.
LOCK TABLE template IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    conflicts TEXT;
BEGIN
    WITH overlapping_labels AS (
        SELECT t.owner, t.purpose, t.audience, label_value.label
        FROM template t
        CROSS JOIN LATERAL unnest(t.labels) AS label_value(label)
        WHERE label_value.label IS NOT NULL
        GROUP BY t.owner, t.purpose, t.audience, label_value.label
        HAVING COUNT(DISTINCT t.id) > 1
    ), overlaps_by_identity AS (
        SELECT owner, purpose, audience, ARRAY_AGG(label ORDER BY label) AS labels
        FROM overlapping_labels
        GROUP BY owner, purpose, audience
    )
    SELECT STRING_AGG(
        FORMAT('owner=%L purpose=%L audience=%L labels=%s', owner, purpose, audience, labels::TEXT),
        '; ' ORDER BY owner, purpose, audience
    )
    INTO conflicts
    FROM overlaps_by_identity;

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
          AND t.purpose = NEW.purpose
          AND t.audience IS NOT DISTINCT FROM NEW.audience
          AND t.labels && NEW.labels
          AND t.id <> NEW.id
    ) THEN
        RAISE EXCEPTION
            'One or more labels already exist for owner %',
            NEW.owner
            USING ERRCODE = 'unique_violation',
                  CONSTRAINT = 'template_owner_purpose_audience_labels_unique';
END IF;
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE TRIGGER trg_check_template_owner_labels_unique
    BEFORE INSERT OR UPDATE OF owner, purpose, audience, labels
                     ON template
                         FOR EACH ROW
                         EXECUTE FUNCTION check_template_owner_labels_unique();

-- Batch-action titles are import identities. Preserve nullable titles for
-- other scheduler jobs, but repair legacy batch actions before enforcing the
-- invariant used by the owner/title uniqueness index.
UPDATE scheduled_task
SET title = 'Untitled batch action ' || id
WHERE event_name = 'invoke-batch-action'
  AND (title IS NULL OR title = '');

ALTER TABLE scheduled_task
    ADD CONSTRAINT chk_scheduled_task_batch_action_title
    CHECK (event_name <> 'invoke-batch-action'
        OR (title IS NOT NULL AND title <> ''));

-- Remove duplicates if already exist
WITH duplicates AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY owner, title
            ORDER BY id
        ) AS rn
    FROM scheduled_task
    WHERE event_name = 'invoke-batch-action'
)
UPDATE scheduled_task st
SET title = st.title || '_' || d.rn
    FROM duplicates d
WHERE st.id = d.id
  AND d.rn > 1;

CREATE UNIQUE INDEX IF NOT EXISTS idx_scheduled_task_owner_title
    ON scheduled_task (owner, title)
    WHERE event_name = 'invoke-batch-action';

-- Lending requests are addressed by this pair when a subsequent ISO message
-- does not contain a supplying-agency request ID. Refuse to install an
-- ambiguous routing identity rather than allowing message lookup to choose an
-- arbitrary aggregate.
LOCK TABLE patron_request IN SHARE ROW EXCLUSIVE MODE;

DO $$
DECLARE
    conflicts TEXT;
BEGIN
    WITH duplicate_identities AS (
        SELECT supplier_symbol, requester_req_id, ARRAY_AGG(id ORDER BY id) AS ids
        FROM patron_request
        WHERE side = 'lending'
          AND supplier_symbol IS NOT NULL
          AND requester_req_id IS NOT NULL
        GROUP BY supplier_symbol, requester_req_id
        HAVING COUNT(*) > 1
    )
    SELECT STRING_AGG(
        FORMAT('supplier_symbol=%L requester_req_id=%L ids=%s', supplier_symbol, requester_req_id, ids::TEXT),
        '; ' ORDER BY supplier_symbol, requester_req_id
    )
    INTO conflicts
    FROM duplicate_identities;

    IF conflicts IS NOT NULL THEN
        RAISE EXCEPTION 'Duplicate lending routing identities already exist: %', conflicts
            USING ERRCODE = 'unique_violation';
    END IF;
END;
$$;

CREATE UNIQUE INDEX idx_patron_request_lending_routing_identity
    ON patron_request (supplier_symbol, requester_req_id)
    WHERE side = 'lending';
