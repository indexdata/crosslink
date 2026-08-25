DROP TRIGGER IF EXISTS trigger_update_patron_request_items ON item;

CREATE OR REPLACE FUNCTION update_patron_request_items()
RETURNS TRIGGER AS $$
BEGIN
UPDATE patron_request
SET items = (
    SELECT jsonb_agg(
                   (to_jsonb(i) - 'pr_id') ||
                   jsonb_build_object(
                           'created_at',
                           to_char(i.created_at, 'YYYY-MM-DD"T"HH24:MI:SS.US') || to_char(i.created_at, 'TZH:TZM')
                   )
           )
    FROM item i
    WHERE i.pr_id = NEW.pr_id
)
WHERE id = NEW.pr_id;

RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_patron_request_items
    AFTER INSERT OR UPDATE ON item
    FOR EACH ROW
    EXECUTE FUNCTION update_patron_request_items();

ALTER TABLE item
    DROP COLUMN lms_request_id;
