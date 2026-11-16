-- Export mod-rs templates and compatible scheduled actions as CrossLink import
-- NDJSON.
--
-- Run this against one mod-rs tenant schema. The database role/search_path must
-- resolve the tables below to that tenant's schema.
--
-- Example:
--   psql "$DATABASE_URL" \
--     --set=ON_ERROR_STOP=1 \
--     --set=owner='ISIL:US-RS1' \
--     --set=unsupported_template_output='unsupported-templates.ndjson' \
--     --file=migration/export-crosslink-config.sql \
--     --quiet --tuples-only --no-align \
--     > crosslink-import.ndjson
--
-- Notes:
--   * Imported scheduled actions are created inactive by CrossLink. Relevant
--     mod-rs timers are therefore exported regardless of their enabled flag so
--     staff can review their migrated configuration before enabling them.
--   * CheckForOverdueSupplierRequests becomes an overdue action.
--   * CheckForStaleSupplierRequests becomes one request-aging action for each
--     positive default, rush, or express threshold configured for the tenant.
--     Blank values use the setting default. CrossLink uses elapsed durations,
--     so mod-rs weekend exclusion cannot be carried across.
--   * PrintPullSlips becomes email-pullslips when it has a non-empty RRULE,
--     valid JSON configuration, and at least one non-empty recipient.
--   * Timer location filters cannot be carried across: CrossLink's
--     patron-request CQL does not expose the legacy mod-rs location identifiers.
--     Ownership is enforced by CrossLink.
--   * Only English localized templates are exported because CrossLink templates
--     do not have a locale field. Known mod-rs Handlebars placeholders are
--     converted to their CrossLink Go-template equivalents in subject and body.
--     Known #if blocks are converted to Go if/end blocks, and triple-brace
--     placeholders are converted to CrossLink's safely escaped placeholders.
--     Templates with residual Handlebars actions or a mixture of patron-request
--     and batch-only placeholders are written to unsupported_template_output
--     for review instead of being included in the import stream.
--   * Template labels are stable and take the form "mod-rs-template-<UUID>",
--     using the localized-template UUID so multiple English templates in one
--     legacy container remain distinct.
--     A pull-slip timer uses the template selected by pull_slip_template_id when
--     an eligible, batch-compatible English template is exported, choosing the
--     lowest localized-template UUID if legacy data contains multiple English
--     children; otherwise CrossLink's default pullslip-email template is exported
--     and used. Migrated templates are retained even when they are not selected.
--   * The attached pull-slip PDF is rendered separately in both systems and is
--     controlled by includePdf.

\set ON_ERROR_STOP on

\if :{?unsupported_template_output}
\else
\set unsupported_template_output 'unsupported-templates.ndjson'
\endif

BEGIN;

-- A malformed task configuration must not abort the entire tenant export.
CREATE OR REPLACE FUNCTION pg_temp.try_parse_jsonb(value TEXT)
    RETURNS JSONB
    LANGUAGE plpgsql
    IMMUTABLE
AS $try_parse_jsonb$
BEGIN
    RETURN value::JSONB;
EXCEPTION
    WHEN invalid_text_representation THEN
        RETURN NULL;
END
$try_parse_jsonb$;

-- Converts one known mod-rs Handlebars placeholder at a time. CrossLink uses
-- html/template for HTML bodies, so triple-brace values deliberately become
-- escaped Go-template fields instead of retaining Handlebars' unescaped output.
CREATE OR REPLACE FUNCTION pg_temp.convert_handlebars_placeholder(
    source TEXT,
    legacy_path TEXT,
    crosslink_path TEXT
)
    RETURNS TEXT
    LANGUAGE plpgsql
    IMMUTABLE
AS $convert_handlebars_placeholder$
DECLARE
    converted TEXT;
    legacy_path_pattern TEXT := replace(legacy_path, '.', '[.]');
    field_replacement TEXT := coalesce(
        '{{' || crosslink_path || '}}',
        'all locations'
    );
    if_replacement TEXT := coalesce(
        '{{if ne ' || crosslink_path || ' "n/a"}}',
        '{{if true}}'
    );
BEGIN
    IF source IS NULL THEN
        RETURN NULL;
    END IF;

    converted := regexp_replace(
        source,
        '[{][{][[:space:]]*/if[[:space:]]*[}][}]',
        '{{end}}',
        'g'
    );
    converted := regexp_replace(
        converted,
        '[{][{][{][[:space:]]*' || legacy_path_pattern
            || '[[:space:]]*[}][}][}]',
        field_replacement,
        'g'
    );
    converted := regexp_replace(
        converted,
        '[{][{][[:space:]]*#if[[:space:]]+' || legacy_path_pattern
            || '[[:space:]]*[}][}]',
        if_replacement,
        'g'
    );

    RETURN regexp_replace(
        converted,
        '[{][{][[:space:]]*' || legacy_path_pattern
            || '[[:space:]]*[}][}]',
        field_replacement,
        'g'
    );
END
$convert_handlebars_placeholder$;

CREATE TEMP TABLE crosslink_config_records ON COMMIT DROP AS
WITH RECURSIVE
effective_settings AS (
    SELECT
        st_key,
        coalesce(
            nullif(btrim(st_value), ''),
            nullif(btrim(st_default_value), '')
        ) AS value
    FROM app_setting
    WHERE st_key IN (
        'pull_slip_template_id',
        'stale_request_2_days',
        'stale_request_rush_hours',
        'stale_request_express_hours'
    )
),
export_settings AS (
    SELECT
        max(value) FILTER (
            WHERE st_key = 'pull_slip_template_id'
        ) AS pull_slip_template_id,
        coalesce(
            max(value) FILTER (
                WHERE st_key = 'stale_request_2_days'
            ),
            '3'
        ) AS stale_request_days,
        max(value) FILTER (
            WHERE st_key = 'stale_request_rush_hours'
        ) AS stale_request_rush_hours,
        max(value) FILTER (
            WHERE st_key = 'stale_request_express_hours'
        ) AS stale_request_express_hours
    FROM effective_settings
),
raw_english_templates AS (
    SELECT
        template_container.tmc_id AS container_id,
        localized_template.ltm_id AS localized_template_id,
        template_container.tmc_name AS title,
        template.tm_header::TEXT AS subject,
        template.tm_template_body AS body
    FROM template_container
    JOIN localized_template
      ON localized_template.ltm_owner_fk = template_container.tmc_id
    JOIN template
      ON template.tm_id = localized_template.ltm_template_fk
    WHERE lower(localized_template.ltm_locality) = 'en'
      AND nullif(btrim(template.tm_template_body), '') IS NOT NULL
),
-- CrossLink does not carry across mod-rs's location filters, so its
-- comma-separated locations placeholder is replaced with "all locations".
-- ActualCount is exposed to batch email templates and can remain selected.
placeholder_mappings(
    step,
    legacy_path,
    crosslink_path,
    available_to_batch_email
) AS (
    VALUES
        (1, 'request.id', '.ReqId', false),
        (2, 'request.cancellationReason', '.CancellationReason', false),
        (3, 'request.pickupLocation', '.PickupLocation', false),
        (4, 'request.pickupURL', '.PickupURL', false),
        (5, 'item.title', '.Title', false),
        (6, 'item.titleOfComponent', '.TitleOfComponent', false),
        (7, 'item.author', '.Author', false),
        (8, 'item.authorOfComponent', '.AuthorOfComponent', false),
        (9, 'item.materialType', '.MaterialType', false),
        (10, 'item.volume', '.Volume', false),
        (11, 'item.issue', '.Issue', false),
        (12, 'item.pages', '.PagesRequested', false),
        (13, 'item.location', '.Location', false),
        (14, 'item.shelvingLocation', '.ShelvingLocation', false),
        (15, 'user.givenName', '.PatronGivenName', false),
        (16, 'user.surname', '.PatronSurname', false),
        (17, 'user.patronProfile', '.PatronProfile', false),
        (18, 'locations', NULL, true),
        (19, 'numRequests', '.ActualCount', true),
        (20, 'user.id', '.PatronId', false),
        (21, 'request.neededBy', '.NeededBy', false)
),
converted_templates AS (
    SELECT
        raw_english_templates.container_id,
        raw_english_templates.localized_template_id,
        raw_english_templates.title,
        raw_english_templates.subject AS source_subject,
        raw_english_templates.body AS source_body,
        raw_english_templates.subject,
        raw_english_templates.body,
        0 AS step
    FROM raw_english_templates

    UNION ALL

    SELECT
        converted_templates.container_id,
        converted_templates.localized_template_id,
        converted_templates.title,
        converted_templates.source_subject,
        converted_templates.source_body,
        pg_temp.convert_handlebars_placeholder(
            converted_templates.subject,
            placeholder_mappings.legacy_path,
            placeholder_mappings.crosslink_path
        ),
        pg_temp.convert_handlebars_placeholder(
            converted_templates.body,
            placeholder_mappings.legacy_path,
            placeholder_mappings.crosslink_path
        ),
        placeholder_mappings.step
    FROM converted_templates
    JOIN placeholder_mappings
      ON placeholder_mappings.step = converted_templates.step + 1
),
template_action_patterns AS (
    SELECT
        '[{][{][[:space:]]*((if[[:space:]]+)?('
            || string_agg(
                replace(placeholder_mappings.crosslink_path, '.', '[.]'),
                '|' ORDER BY placeholder_mappings.step
            )
            || ')|if[[:space:]]+ne[[:space:]]+('
            || string_agg(
                replace(placeholder_mappings.crosslink_path, '.', '[.]'),
                '|' ORDER BY placeholder_mappings.step
            )
            || ')[[:space:]]+"n/a"|if[[:space:]]+true|else|end)'
            || '[[:space:]]*[}][}]'
            AS supported_action_pattern
    FROM placeholder_mappings
    WHERE placeholder_mappings.crosslink_path IS NOT NULL
),
converted_english_templates AS (
    SELECT
        converted_templates.container_id,
        converted_templates.localized_template_id,
        converted_templates.title,
        converted_templates.source_subject,
        converted_templates.source_body,
        converted_templates.subject,
        converted_templates.body
    FROM converted_templates
    WHERE converted_templates.step = (
        SELECT count(*)
        FROM placeholder_mappings
    )
),
template_characteristics AS (
    SELECT
        converted_english_templates.*,
        NOT (
            regexp_replace(
                coalesce(converted_english_templates.subject, ''),
                template_action_patterns.supported_action_pattern,
                '',
                'g'
            ) ~ '[{][{]'
            OR regexp_replace(
                converted_english_templates.body,
                template_action_patterns.supported_action_pattern,
                '',
                'g'
            ) ~ '[{][{]'
        ) AS syntax_compatible,
        NOT EXISTS (
            SELECT 1
            FROM placeholder_mappings
            WHERE placeholder_mappings.crosslink_path IS NOT NULL
              AND placeholder_mappings.available_to_batch_email
              AND (
                  coalesce(converted_english_templates.subject, '') ~ (
                      '[{][{][^}]*'
                      || replace(
                          placeholder_mappings.crosslink_path,
                          '.',
                          '[.]'
                      )
                      || '([^[:alnum:]_]|[}])'
                  )
                  OR converted_english_templates.body ~ (
                      '[{][{][^}]*'
                      || replace(
                          placeholder_mappings.crosslink_path,
                          '.',
                          '[.]'
                      )
                      || '([^[:alnum:]_]|[}])'
                  )
              )
        ) AS patron_compatible,
        NOT EXISTS (
            SELECT 1
            FROM placeholder_mappings
            WHERE placeholder_mappings.crosslink_path IS NOT NULL
              AND NOT placeholder_mappings.available_to_batch_email
              AND (
                  coalesce(converted_english_templates.subject, '') ~ (
                      '[{][{][^}]*'
                      || replace(
                          placeholder_mappings.crosslink_path,
                          '.',
                          '[.]'
                      )
                      || '([^[:alnum:]_]|[}])'
                  )
                  OR converted_english_templates.body ~ (
                      '[{][{][^}]*'
                      || replace(
                          placeholder_mappings.crosslink_path,
                          '.',
                          '[.]'
                      )
                      || '([^[:alnum:]_]|[}])'
                  )
              )
        ) AS batch_data_compatible
    FROM converted_english_templates
    CROSS JOIN template_action_patterns
),
english_templates AS (
    SELECT
        template_characteristics.*,
        template_characteristics.syntax_compatible
            AND (
                template_characteristics.patron_compatible
                OR template_characteristics.batch_data_compatible
            ) AS import_compatible,
        template_characteristics.syntax_compatible
            AND nullif(btrim(template_characteristics.subject), '') IS NOT NULL
            AND template_characteristics.batch_data_compatible
            AS batch_compatible,
        concat_ws(
            '; ',
            CASE
                WHEN NOT template_characteristics.syntax_compatible THEN
                    'unsupported template action remains after conversion'
            END,
            CASE
                WHEN NOT template_characteristics.patron_compatible
                 AND NOT template_characteristics.batch_data_compatible THEN
                    'mixes patron-request and batch-only placeholders'
            END
        ) AS incompatibility_reason
    FROM template_characteristics
),
compatible_timers AS (
    SELECT
        timer.tr_id AS timer_id,
        nullif(btrim(timer.tr_code), '') AS code,
        nullif(btrim(timer.tr_description), '') AS description,
        lower(timer.tr_task_code) AS task_code,
        regexp_replace(btrim(timer.tr_rrule), '^RRULE:', '', 'i') AS schedule,
        pg_temp.try_parse_jsonb(nullif(btrim(timer.tr_task_config), '')) AS config,
        count(*) OVER (
            PARTITION BY lower(timer.tr_task_code)
        ) AS task_count
    FROM timer
    WHERE lower(timer.tr_task_code) IN (
        lower('CheckForOverdueSupplierRequests'),
        lower('CheckForStaleSupplierRequests'),
        lower('PrintPullSlips')
    )
      AND nullif(btrim(timer.tr_rrule), '') IS NOT NULL
),
pull_slip_timers AS (
    SELECT
        compatible_timers.timer_id,
        CASE
            WHEN compatible_timers.task_count > 1 THEN
                coalesce(
                    compatible_timers.description,
                    compatible_timers.code,
                    'Imported mod-rs pull-slip schedule'
                ) || ' (' || compatible_timers.timer_id || ')'
            ELSE coalesce(
                compatible_timers.description,
                compatible_timers.code,
                'Imported mod-rs pull-slip schedule'
            )
        END AS title,
        compatible_timers.schedule,
        compatible_timers.config,
        CASE
            WHEN jsonb_typeof(
                compatible_timers.config -> 'emailAddresses'
            ) = 'array' THEN
                compatible_timers.config -> 'emailAddresses'
            ELSE '[]'::JSONB
        END AS email_addresses,
        coalesce(
            (
                SELECT CASE
                    WHEN english_templates.batch_compatible THEN
                        'mod-rs-template-'
                            || english_templates.localized_template_id
                END
                FROM english_templates
                WHERE english_templates.container_id::TEXT =
                    export_settings.pull_slip_template_id
                ORDER BY english_templates.localized_template_id
                LIMIT 1
            ),
            'pullslip-email'
        ) AS template_label
    FROM compatible_timers
    CROSS JOIN export_settings
    WHERE compatible_timers.task_code = lower('PrintPullSlips')
),
exportable_pull_slip_timers AS (
    SELECT *
    FROM pull_slip_timers
    WHERE jsonb_array_length(email_addresses) > 0
      AND NOT EXISTS (
          SELECT 1
          FROM jsonb_array_elements(email_addresses) AS address(value)
          WHERE jsonb_typeof(address.value) <> 'string'
             OR nullif(btrim(address.value #>> '{}'), '') IS NULL
      )
),
aging_levels AS (
    SELECT
        level_config.level,
        level_config.title,
        level_config.batch_query,
        level_config.threshold_value::NUMERIC
            * level_config.hours_multiplier AS interval_hours
    FROM export_settings
    CROSS JOIN LATERAL (
        VALUES
            (
                'all',
                'Request aging of default requests',
                'side = lending and (state = VALIDATED or state = ITEM_PENDING or state = WILL_SUPPLY_PENDING or state = WILL_SUPPLY or state = CONDITION_PENDING or state = SEARCHING) and ((cql.allRecords = 1 not service_level = "") or (service_level <> Rush and service_level <> Express))',
                export_settings.stale_request_days,
                24
            ),
            (
                'rush',
                'Request aging of rush requests',
                'side = lending and (state = VALIDATED or state = ITEM_PENDING or state = WILL_SUPPLY_PENDING or state = WILL_SUPPLY or state = CONDITION_PENDING or state = SEARCHING) and service_level = Rush',
                export_settings.stale_request_rush_hours,
                1
            ),
            (
                'express',
                'Request aging of express requests',
                'side = lending and (state = VALIDATED or state = ITEM_PENDING or state = WILL_SUPPLY_PENDING or state = WILL_SUPPLY or state = CONDITION_PENDING or state = SEARCHING) and service_level = Express',
                export_settings.stale_request_express_hours,
                1
            )
    ) AS level_config(
        level,
        title,
        batch_query,
        threshold_value,
        hours_multiplier
    )
    WHERE level_config.threshold_value ~ '^[0-9]+$'
      AND level_config.threshold_value::NUMERIC
            * level_config.hours_multiplier BETWEEN 1 AND 2562047
),
overdue_actions AS (
    SELECT
        compatible_timers.timer_id,
        CASE
            WHEN compatible_timers.task_count > 1 THEN
                'Overdue loans (' || compatible_timers.timer_id || ')'
            ELSE 'Overdue loans'
        END AS title,
        compatible_timers.schedule
    FROM compatible_timers
    WHERE compatible_timers.task_code =
        lower('CheckForOverdueSupplierRequests')
),
request_aging_actions AS (
    SELECT
        compatible_timers.timer_id,
        aging_levels.level,
        CASE
            WHEN compatible_timers.task_count > 1 THEN
                aging_levels.title || ' (' || compatible_timers.timer_id || ')'
            ELSE aging_levels.title
        END AS title,
        aging_levels.batch_query,
        compatible_timers.schedule,
        aging_levels.interval_hours::TEXT || 'h' AS interval
    FROM compatible_timers
    CROSS JOIN aging_levels
    WHERE compatible_timers.task_code =
        lower('CheckForStaleSupplierRequests')
),
export_records AS (
    SELECT
        1 AS record_type_order,
        english_templates.localized_template_id::TEXT AS record_order,
        'template'::TEXT AS record_type,
        'mod-rs-template-'
            || english_templates.localized_template_id AS record_id,
        true AS importable,
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
                    'mod-rs-template-'
                        || english_templates.localized_template_id
                ),
                'audience', CASE
                    WHEN english_templates.container_id::TEXT =
                        export_settings.pull_slip_template_id THEN 'staff'
                    ELSE NULL
                END
            ))
        ) AS record
    FROM english_templates
    CROSS JOIN export_settings
    WHERE english_templates.import_compatible

    UNION ALL

    SELECT
        1 AS record_type_order,
        english_templates.localized_template_id::TEXT AS record_order,
        'unsupportedTemplate'::TEXT AS record_type,
        english_templates.localized_template_id::TEXT AS record_id,
        false AS importable,
        jsonb_build_object(
            'type', 'unsupportedTemplate',
            'owner', :'owner',
            'data', jsonb_strip_nulls(jsonb_build_object(
                'containerId', english_templates.container_id,
                'localizedTemplateId',
                    english_templates.localized_template_id,
                'title', english_templates.title,
                'reason', english_templates.incompatibility_reason,
                'source', jsonb_strip_nulls(jsonb_build_object(
                    'subject', english_templates.source_subject,
                    'body', english_templates.source_body
                )),
                'converted', jsonb_strip_nulls(jsonb_build_object(
                    'subject', english_templates.subject,
                    'body', english_templates.body
                ))
            ))
        ) AS record
    FROM english_templates
    WHERE NOT english_templates.import_compatible

    UNION ALL

    SELECT
        2 AS record_type_order,
        'pullslip-email' AS record_order,
        'template'::TEXT AS record_type,
        'pullslip-email'::TEXT AS record_id,
        true AS importable,
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
        overdue_actions.timer_id::TEXT AS record_order,
        'batchAction'::TEXT AS record_type,
        overdue_actions.timer_id::TEXT AS record_id,
        true AS importable,
        jsonb_build_object(
            'type', 'batchAction',
            'owner', :'owner',
            'data', jsonb_build_object(
                'actionName', 'overdue',
                'title', overdue_actions.title,
                'batchQuery',
                    'side = lending and (state = RECEIVED or state = RENEWED)',
                'schedule', overdue_actions.schedule
            )
        ) AS record
    FROM overdue_actions

    UNION ALL

    SELECT
        4 AS record_type_order,
        pull_slip_timers.timer_id::TEXT AS record_order,
        'batchAction'::TEXT AS record_type,
        pull_slip_timers.timer_id::TEXT AS record_id,
        true AS importable,
        jsonb_build_object(
            'type', 'batchAction',
            'owner', :'owner',
            'data', jsonb_build_object(
                'actionName', 'email-pullslips',
                'title', pull_slip_timers.title,
                'batchQuery', 'side = lending and state = WILL_SUPPLY',
                'schedule', pull_slip_timers.schedule,
                'actionParams', jsonb_build_object(
                    'to', pull_slip_timers.email_addresses,
                    'templateLabel', pull_slip_timers.template_label,
                    'includePdf', CASE lower(
                        pull_slip_timers.config ->> 'attachPullSlips'
                    )
                        WHEN 'true' THEN true
                        ELSE false
                    END
                )
            )
        ) AS record
    FROM exportable_pull_slip_timers AS pull_slip_timers

    UNION ALL

    SELECT
        5 AS record_type_order,
        request_aging_actions.timer_id || '-'
            || request_aging_actions.level AS record_order,
        'batchAction'::TEXT AS record_type,
        request_aging_actions.timer_id || '-'
            || request_aging_actions.level AS record_id,
        true AS importable,
        jsonb_build_object(
            'type', 'batchAction',
            'owner', :'owner',
            'data', jsonb_build_object(
                'actionName', 'request-aging',
                'title', request_aging_actions.title,
                'batchQuery', request_aging_actions.batch_query,
                'schedule', request_aging_actions.schedule,
                'actionParams', jsonb_build_object(
                    'interval', request_aging_actions.interval
                )
            )
        ) AS record
    FROM request_aging_actions
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
            || octet_length(record::TEXT) || ' bytes)',
        E'\n' ORDER BY record_type_order, record_order
    )
    INTO problems
    FROM crosslink_config_records
    WHERE importable
      AND octet_length(record::TEXT) > 1048576;

    IF problems IS NOT NULL THEN
        RAISE EXCEPTION
            E'CrossLink config export records exceed the 1048576-byte import limit:\n%',
            problems;
    END IF;
END
$record_size_preflight$;

SELECT record::TEXT
FROM crosslink_config_records
WHERE NOT importable
ORDER BY record_type_order, record_order
\g :unsupported_template_output

SELECT record::TEXT
FROM crosslink_config_records
WHERE importable
ORDER BY record_type_order, record_order;

ROLLBACK;
