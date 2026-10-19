ALTER TABLE item
    ADD COLUMN lms_request_id VARCHAR;

UPDATE item i
SET lms_request_id = pr.ill_request -> 'header' ->> 'requestingAgencyRequestId'
FROM patron_request pr
WHERE i.pr_id = pr.id
  AND pr.side = 'lending'
  AND COALESCE(pr.ill_request -> 'header' ->> 'requestingAgencyRequestId', '') <> '';

CREATE OR REPLACE FUNCTION update_patron_request_items()
RETURNS TRIGGER AS $$
DECLARE
    target_pr_id VARCHAR;
BEGIN
IF TG_OP = 'DELETE' THEN
    target_pr_id := OLD.pr_id;
ELSE
    target_pr_id := NEW.pr_id;
END IF;

UPDATE patron_request
SET items = COALESCE(
        (
            SELECT jsonb_agg(
                           (to_jsonb(i) - 'pr_id') ||
                           jsonb_build_object(
                                   'created_at',
                                   to_char(i.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US') || to_char(i.created_at, 'TZH:TZM')
                           )
                   )
            FROM item i
            WHERE i.pr_id = target_pr_id
        ),
        '[]'::jsonb
            )
WHERE id = target_pr_id;

IF TG_OP = 'DELETE' THEN
    RETURN OLD;
END IF;
RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER trigger_update_patron_request_items ON item;

CREATE TRIGGER trigger_update_patron_request_items
    AFTER INSERT OR UPDATE OR DELETE ON item
    FOR EACH ROW
    EXECUTE FUNCTION update_patron_request_items();
