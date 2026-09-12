-- Export mod-rs templates and compatible timers as CrossLink import NDJSON.
--
-- Run this against one mod-rs tenant schema. The database role/search_path must
-- resolve the tables below to that tenant's schema.
--
-- Example:
--   psql "$DATABASE_URL" \
--     --set=ON_ERROR_STOP=1 \
--     --set=owner='ISIL:US-RS1' \
--     --file=migration/export-crosslink-config.sql \
--     --quiet --tuples-only --no-align \
--     > crosslink-import.ndjson
--
-- Notes:
--   * Only English localized templates are exported because CrossLink templates
--     do not have a locale field.
--   * Template labels are stable and take the form "mod-rs-template-<UUID>".
--   * Legacy template containers are email templates. A pull-slip timer uses its
--     configured template only when an eligible English template is exported;
--     otherwise this script exports CrossLink's default pullslip-email template.
--     The attached pull-slip PDF is rendered separately in both systems and is
--     controlled by includePdf.
--   * Only enabled PrintPullSlips timers with a non-empty RRULE and at least one
--     recipient are compatible with CrossLink batch actions.
--   * Timer location filters cannot be carried across: CrossLink's patron-request
--     CQL does not expose the legacy mod-rs location identifiers. Ownership is
--     enforced by CrossLink, and the exported query selects lending requests in
--     WILL_SUPPLY state.
--   * Other mod-rs timers are internal maintenance/network jobs and are skipped.
--   * Legacy Handlebars template expressions are preserved verbatim. Review any
--     custom expressions because CrossLink uses Go-template expressions.

\set ON_ERROR_STOP on

BEGIN;

CREATE TEMP TABLE crosslink_config_records ON COMMIT DROP AS
WITH export_settings AS (
    SELECT max(st_value) FILTER (
        WHERE st_key = 'pull_slip_template_id'
    ) AS pull_slip_template_id
    FROM app_setting
),
english_templates AS (
    SELECT
        template_container.tmc_id AS container_id,
        template_container.tmc_name AS title,
        template.tm_header AS subject,
        template.tm_template_body AS body
    FROM template_container
    JOIN localized_template
      ON localized_template.ltm_owner_fk = template_container.tmc_id
    JOIN template
      ON template.tm_id = localized_template.ltm_template_fk
    WHERE lower(localized_template.ltm_locality) = 'en'
      AND nullif(btrim(template.tm_template_body), '') IS NOT NULL
),
pull_slip_timers AS (
    SELECT
        timer.tr_id AS timer_id,
        coalesce(
            nullif(btrim(timer.tr_description), ''),
            nullif(btrim(timer.tr_code), ''),
            'Imported mod-rs pull-slip schedule'
        ) AS title,
        regexp_replace(timer.tr_rrule, '^RRULE:', '', 'i') AS schedule,
        timer.tr_task_config::jsonb AS config,
        CASE
            WHEN EXISTS (
                SELECT 1
                FROM english_templates
                WHERE english_templates.container_id::text =
                    nullif(btrim(export_settings.pull_slip_template_id), '')
            ) THEN 'mod-rs-template-'
                || btrim(export_settings.pull_slip_template_id)
            ELSE 'pullslip-email'
        END AS template_label
    FROM timer
    CROSS JOIN export_settings
    WHERE timer.tr_enabled IS TRUE
      AND lower(timer.tr_task_code) = lower('PrintPullSlips')
      AND nullif(btrim(timer.tr_rrule), '') IS NOT NULL
      AND nullif(btrim(timer.tr_task_config), '') IS NOT NULL
),
exportable_pull_slip_timers AS (
    SELECT *
    FROM pull_slip_timers
    WHERE CASE
        WHEN jsonb_typeof(config) IS DISTINCT FROM 'object' THEN false
        WHEN jsonb_typeof(config -> 'emailAddresses')
            IS DISTINCT FROM 'array' THEN false
        ELSE jsonb_array_length(config -> 'emailAddresses') > 0
             AND NOT EXISTS (
                 SELECT 1
                 FROM jsonb_array_elements(
                     config -> 'emailAddresses'
                 ) AS address(value)
                 WHERE jsonb_typeof(address.value)
                     IS DISTINCT FROM 'string'
             )
    END
),
export_records AS (
    SELECT
        1 AS record_type_order,
        english_templates.container_id AS record_order,
        'template'::text AS record_type,
        'mod-rs-template-' || english_templates.container_id AS record_id,
        jsonb_build_object(
            'type', 'template',
            'owner', :'owner',
            'data', jsonb_strip_nulls(jsonb_build_object(
                'title', english_templates.title,
                'purpose', 'email',
                'subject', english_templates.subject,
                'body', english_templates.body,
                'contentType', 'html',
                'labels', jsonb_build_array(
                    'mod-rs-template-' || english_templates.container_id
                )
            ))
        ) AS record
    FROM english_templates

    UNION ALL

    SELECT
        2 AS record_type_order,
        NULL AS record_order,
        'template'::text AS record_type,
        'pullslip-email'::text AS record_id,
        jsonb_build_object(
            'type', 'template',
            'owner', :'owner',
            'data', jsonb_build_object(
                'title', 'Scheduled pullslips email template',
                'purpose', 'email',
                'subject', 'Scheduled pullslips',
                'body', '<p>This is an automated pull slip summary.</p>
<p>
  <strong>Query:</strong> {{.BatchQuery}}<br>
  <strong>Matching requests:</strong> {{.ActualCount}} (of {{.FullCount}} total)
</p>
<p>Please process the attached pull slips at your earliest convenience.</p>
',
                'contentType', 'html',
                'labels', jsonb_build_array('pullslip-email'),
                'audience', 'staff'
            )
        ) AS record
    WHERE EXISTS (
        SELECT 1
        FROM exportable_pull_slip_timers
        WHERE template_label = 'pullslip-email'
    )

    UNION ALL

    SELECT
        3 AS record_type_order,
        pull_slip_timers.timer_id AS record_order,
        'batchAction'::text AS record_type,
        pull_slip_timers.timer_id::text AS record_id,
        jsonb_build_object(
            'type', 'batchAction',
            'owner', :'owner',
            'data', jsonb_build_object(
                'actionName', 'email-pullslips',
                'title', pull_slip_timers.title,
                'batchQuery', 'side = lending and state = WILL_SUPPLY',
                'schedule', pull_slip_timers.schedule,
                'actionParams', jsonb_build_object(
                    'to', pull_slip_timers.config -> 'emailAddresses',
                    'templateLabel', pull_slip_timers.template_label,
                    'includePdf', coalesce(
                        (pull_slip_timers.config ->> 'attachPullSlips')::boolean,
                        false
                    )
                )
            )
        ) AS record
    FROM exportable_pull_slip_timers AS pull_slip_timers
)
SELECT *
FROM export_records;

-- Keep this boundary in sync with broker/import/service.maxImportRecordBytes.
DO $record_size_preflight$
DECLARE
    problems TEXT;
BEGIN
    SELECT string_agg(
        record_type || ' ' || record_id || ' ('
            || octet_length(record::text) || ' bytes)',
        E'\n' ORDER BY record_type_order, record_order
    )
    INTO problems
    FROM crosslink_config_records
    WHERE octet_length(record::text) > 1048576;

    IF problems IS NOT NULL THEN
        RAISE EXCEPTION
            E'CrossLink config export records exceed the 1048576-byte import limit:\n%',
            problems;
    END IF;
END
$record_size_preflight$;

SELECT record::text
FROM crosslink_config_records
ORDER BY record_type_order, record_order;

ROLLBACK;
