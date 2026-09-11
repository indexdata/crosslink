-- Export open mod-rs patron requests as CrossLink import NDJSON.
--
-- Run this against one mod-rs tenant schema. The database role/search_path must
-- resolve the tables below to that tenant's schema. The owner must be the local
-- institution's canonical symbol and must already exist in CrossLink's directory.
--
-- Example:
--   psql "$DATABASE_URL" \
--     --set=ON_ERROR_STOP=1 \
--     --set=owner='ISIL:US-RS1' \
--     --file=migration/export-crosslink-open-patron-requests.sql \
--     --quiet --tuples-only --no-align \
--     > crosslink-open-patron-requests.ndjson
--
-- Import into CrossLink with conflictPolicy=fail for the initial migration.
-- The CrossLink importer limits each NDJSON record to 1 MiB.
--
-- Migration policy:
--   * "Open" means the request's state_model_status row is non-terminal.
--   * Both borrowing (requester) and lending (supplier) requests are exported.
--   * Standard mod-rs, nonreturnable, and SLNP states are mapped to the closest
--     state in CrossLink's "default" model. Review the mapping below before use.
--   * The ISO18626 request is reconstructed from normalized mod-rs columns.
--   * A selected item is emitted only when pr_selected_item_barcode is present.
--   * Borrowing requests include a synthesized ILL transaction and their rota as
--     located suppliers. Lending requests must not include an ILL transaction.
--   * Notifications without enough information to identify both parties are
--     omitted. Loan conditions are exported as condition notifications.
--   * Audit history, protocol audit payloads, batches, and queued jobs are not
--     represented by CrossLink's patron-request import contract and are omitted.
--   * mod-rs timestamps are treated as UTC.
--
-- The preflight block runs before any NDJSON is emitted and stops the export if
-- an open request has an unmapped state or is missing required import data.

\set ON_ERROR_STOP on

BEGIN;

CREATE TEMP TABLE crosslink_state_map (
    legacy_state TEXT NOT NULL,
    side TEXT NOT NULL,
    crosslink_state TEXT NOT NULL,
    PRIMARY KEY (legacy_state, side)
) ON COMMIT DROP;

INSERT INTO crosslink_state_map (legacy_state, side, crosslink_state) VALUES
    -- Borrowing/requester states
    ('REQ_IDLE',                         'borrowing', 'NEW'),
    ('REQ_VALIDATED',                    'borrowing', 'VALIDATED'),
    ('REQ_INVALID_PATRON',               'borrowing', 'INVALID_PATRON'),
    ('REQ_SOURCING_ITEM',                'borrowing', 'READY_TO_SEND'),
    ('REQ_SUPPLIER_IDENTIFIED',           'borrowing', 'SUPPLIER_LOCATED'),
    ('REQ_REQUEST_SENT_TO_SUPPLIER',     'borrowing', 'SENT'),
    ('REQ_CONDITIONAL_ANSWER_RECEIVED',  'borrowing', 'CONDITION_PENDING'),
    ('REQ_CANCEL_PENDING',               'borrowing', 'CANCEL_PENDING'),
    ('REQ_CANCELLED_WITH_SUPPLIER',      'borrowing', 'RETRY_PENDING'),
    ('REQ_UNABLE_TO_CONTACT_SUPPLIER',   'borrowing', 'RETRY_PENDING'),
    ('REQ_PENDING',                      'borrowing', 'SENT'),
    ('REQ_WILL_SUPPLY',                  'borrowing', 'WILL_SUPPLY'),
    ('REQ_EXPECTS_TO_SUPPLY',            'borrowing', 'WILL_SUPPLY'),
    ('REQ_SHIPPED',                      'borrowing', 'SHIPPED'),
    ('REQ_BORROWING_LIBRARY_RECEIVED',   'borrowing', 'RECEIVED'),
    ('REQ_LOANED_DIGITALLY',             'borrowing', 'CHECKED_OUT'),
    ('REQ_OVERDUE',                      'borrowing', 'CHECKED_OUT'),
    ('REQ_RECALLED',                     'borrowing', 'CHECKED_OUT'),
    ('REQ_AWAITING_RETURN_SHIPPING',     'borrowing', 'CHECKED_IN'),
    ('REQ_CHECKED_IN',                   'borrowing', 'CHECKED_IN'),
    ('REQ_SHIPPED_TO_SUPPLIER',          'borrowing', 'SHIPPED_RETURNED'),
    ('REQ_BORROWER_RETURNED',            'borrowing', 'SHIPPED_RETURNED'),
    ('REQ_LOCAL_REVIEW',                 'borrowing', 'NEEDS_REVIEW'),
    ('REQ_BLANK_FORM_REVIEW',            'borrowing', 'NEEDS_REVIEW'),
    ('REQ_DUPLICATE_REVIEW',             'borrowing', 'DUPLICATE'),
    ('REQ_ERROR',                        'borrowing', 'NEEDS_REVIEW'),
    ('SLNP_REQ_IDLE',                    'borrowing', 'NEW'),
    ('SLNP_REQ_ABORTED',                 'borrowing', 'NEEDS_REVIEW'),
    ('SLNP_REQ_SHIPPED',                 'borrowing', 'SHIPPED'),
    ('SLNP_REQ_CHECKED_IN',              'borrowing', 'CHECKED_IN'),
    ('SLNP_REQ_AWAITING_RETURN_SHIPPING','borrowing', 'CHECKED_IN'),
    ('SLNP_REQ_ITEM_LOST',               'borrowing', 'NEEDS_REVIEW'),
    ('SLNP_REQ_PATRON_INVALID',          'borrowing', 'INVALID_PATRON'),
    ('SLNP_REQ_DOCUMENT_AVAILABLE',      'borrowing', 'RECEIVED'),

    -- Lending/supplier states
    ('RES_IDLE',                         'lending', 'NEW'),
    ('RES_PENDING_CONDITIONAL_ANSWER',   'lending', 'CONDITION_PENDING'),
    ('RES_NEW_AWAIT_PULL_SLIP',          'lending', 'WILL_SUPPLY'),
    ('RES_AWAIT_PICKING',                'lending', 'ITEM_PENDING'),
    ('RES_COPY_AWAIT_PICKING',           'lending', 'WILL_SUPPLY'),
    ('RES_SEQUESTERED',                  'lending', 'ITEM_PENDING'),
    ('RES_AWAIT_PROXY_BORROWER',         'lending', 'ITEM_PENDING'),
    ('RES_HOLD_PLACED',                  'lending', 'ITEM_PENDING'),
    ('RES_AWAIT_SHIP',                   'lending', 'WILL_SUPPLY_PENDING'),
    ('RES_ITEM_SHIPPED',                 'lending', 'SHIPPED'),
    ('RES_LOANED_DIGITALLY',             'lending', 'SHIPPED'),
    ('RES_ITEM_RETURNED',                'lending', 'RECEIVED'),
    ('RES_CHECKED_IN_TO_RESHARE',        'lending', 'RECEIVED'),
    ('RES_AWAITING_RETURN_SHIPPING',     'lending', 'SHIPPED_RETURN'),
    ('RES_AWAIT_DESEQUESTRATION',        'lending', 'SHIPPED_RETURN'),
    ('RES_OVERDUE',                      'lending', 'SHIPPED_RETURN'),
    ('RES_CANCEL_REQUEST_RECEIVED',      'lending', 'CANCEL_REQUESTED'),
    ('RES_ERROR',                        'lending', 'ITEM_PENDING'),
    ('SLNP_RES_IDLE',                    'lending', 'NEW'),
    ('SLNP_RES_ABORTED',                 'lending', 'ITEM_PENDING'),
    ('SLNP_RES_NEW_AWAIT_PULL_SLIP',     'lending', 'WILL_SUPPLY'),
    ('SLNP_RES_AWAIT_PICKING',           'lending', 'ITEM_PENDING'),
    ('SLNP_RES_AWAIT_SHIP',              'lending', 'WILL_SUPPLY_PENDING'),
    ('SLNP_RES_ITEM_SHIPPED',            'lending', 'SHIPPED');

CREATE TEMP VIEW crosslink_request_ids AS
SELECT
    patron_request.pr_id AS legacy_id,
    CASE
        WHEN patron_request.pr_is_requester IS NOT TRUE
            THEN patron_request.pr_id
        WHEN state_model.sm_shortcode IN ('SLNPRequester', 'SLNPResponder',
                                          'SLNPNonReturnableRequester',
                                          'SLNPNonReturnableResponder')
            THEN coalesce(patron_request.pr_hrid, patron_request.pr_id)
        ELSE coalesce(patron_request.pr_hrid, patron_request.pr_id)
             || '~' || coalesce(patron_request.pr_rota_position::text, 'norota')
    END AS import_id
FROM patron_request
JOIN state_model
  ON state_model.sm_id = patron_request.pr_state_model_fk;

CREATE TEMP VIEW crosslink_open_requests AS
SELECT
    patron_request.*,
    request_identity.import_id AS import_patron_request_id,
    next_request_identity.import_id AS import_next_request_id,
    previous_request_identity.import_id AS import_previous_request_id,
    status.st_code AS legacy_state,
    coalesce(patron_request.pr_needs_attention, status.st_needs_attention, false)
        AS import_needs_attention,
    state_model.sm_shortcode AS legacy_state_model,
    CASE
        WHEN patron_request.pr_is_requester IS TRUE THEN 'borrowing'
        ELSE 'lending'
    END AS import_side,
    crosslink_state_map.crosslink_state AS import_state,
    CASE lower(replace(service_type.rdv_value, ' ', ''))
        WHEN 'loan' THEN 'Loan'
        WHEN 'copy' THEN 'Copy'
        WHEN 'copyorloan' THEN 'CopyOrLoan'
    END AS import_service_type,
    service_level.rdv_value AS import_service_level,
    publication_type.rdv_value AS import_publication_type,
    copyright_type.rdv_value AS import_copyright_type,
    maximum_cost_currency.rdv_value AS import_maximum_cost_currency,
    cost_currency.rdv_value AS import_cost_currency,
    CASE
        WHEN patron_request.pr_is_requester IS NOT TRUE
            THEN patron_request.pr_peer_request_identifier
        ELSE request_identity.import_id
    END AS import_requester_request_id,
    CASE
        WHEN patron_request.pr_is_requester IS NOT TRUE
            THEN patron_request.pr_id
        ELSE patron_request.pr_peer_request_identifier
    END AS import_supplier_request_id
FROM patron_request
JOIN crosslink_request_ids AS request_identity
  ON request_identity.legacy_id = patron_request.pr_id
LEFT JOIN crosslink_request_ids AS next_request_identity
  ON next_request_identity.legacy_id = patron_request.pr_succeeded_by_fk
LEFT JOIN crosslink_request_ids AS previous_request_identity
  ON previous_request_identity.legacy_id = patron_request.pr_preceded_by_fk
JOIN status
  ON status.st_id = patron_request.pr_state_fk
JOIN state_model
  ON state_model.sm_id = patron_request.pr_state_model_fk
JOIN state_model_status
  ON state_model_status.sms_state_model = patron_request.pr_state_model_fk
 AND state_model_status.sms_state = patron_request.pr_state_fk
LEFT JOIN crosslink_state_map
  ON crosslink_state_map.legacy_state = status.st_code
 AND crosslink_state_map.side = CASE
        WHEN patron_request.pr_is_requester IS TRUE THEN 'borrowing'
        ELSE 'lending'
     END
LEFT JOIN refdata_value AS service_type
  ON service_type.rdv_id = patron_request.pr_service_type_fk
LEFT JOIN refdata_value AS service_level
  ON service_level.rdv_id = patron_request.pr_service_level_fk
LEFT JOIN refdata_value AS publication_type
  ON publication_type.rdv_id = patron_request.pr_pub_type_fk
LEFT JOIN refdata_value AS copyright_type
  ON copyright_type.rdv_id = patron_request.pr_copyright_type_fk
LEFT JOIN refdata_value AS maximum_cost_currency
  ON maximum_cost_currency.rdv_id = patron_request.pr_maximum_costs_code_fk
LEFT JOIN refdata_value AS cost_currency
  ON cost_currency.rdv_id = patron_request.pr_cost_currency_fk
WHERE state_model_status.sms_is_terminal IS FALSE;

DO $preflight$
DECLARE
    problems TEXT;
BEGIN
    SELECT string_agg(problem, E'\n' ORDER BY problem)
    INTO problems
    FROM (
        SELECT DISTINCT
            'unmapped open state: ' || legacy_state || ' (' || import_side || ')'
                AS problem
        FROM crosslink_open_requests
        WHERE import_state IS NULL

        UNION ALL

        SELECT 'request ' || pr_id || ': missing creation or update timestamp'
        FROM crosslink_open_requests
        WHERE pr_date_created IS NULL OR pr_last_updated IS NULL

        UNION ALL

        SELECT 'request ' || pr_id || ': missing canonical requester symbol'
        FROM crosslink_open_requests
        WHERE nullif(btrim(pr_req_inst_symbol), '') IS NULL
           OR position(':' IN pr_req_inst_symbol) = 0

        UNION ALL

        SELECT 'request ' || pr_id || ': missing canonical supplier symbol'
        FROM crosslink_open_requests
        WHERE import_side = 'lending'
          AND (nullif(btrim(pr_sup_inst_symbol), '') IS NULL
               OR position(':' IN pr_sup_inst_symbol) = 0)

        UNION ALL

        SELECT 'request ' || pr_id || ': unsupported or missing service type'
        FROM crosslink_open_requests
        WHERE import_service_type IS NULL

        UNION ALL

        SELECT 'request ' || pr_id || ': missing requester request ID'
        FROM crosslink_open_requests
        WHERE nullif(btrim(import_requester_request_id), '') IS NULL
    ) AS validation;

    IF problems IS NOT NULL THEN
        RAISE EXCEPTION E'CrossLink patron-request export preflight failed:\n%', problems;
    END IF;
END
$preflight$;

WITH request_payloads AS (
    SELECT
        request.*,
        jsonb_strip_nulls(jsonb_build_object(
            'header', jsonb_strip_nulls(jsonb_build_object(
                'requestingAgencyId', jsonb_build_object(
                    'agencyIdType', jsonb_build_object(
                        '#text', split_part(request.pr_req_inst_symbol, ':', 1)
                    ),
                    'agencyIdValue', regexp_replace(
                        request.pr_req_inst_symbol, '^[^:]+:', ''
                    )
                ),
                'supplyingAgencyId', CASE
                    WHEN nullif(btrim(request.pr_sup_inst_symbol), '') IS NULL
                        THEN NULL
                    ELSE jsonb_build_object(
                        'agencyIdType', jsonb_build_object(
                            '#text', split_part(request.pr_sup_inst_symbol, ':', 1)
                        ),
                        'agencyIdValue', regexp_replace(
                            request.pr_sup_inst_symbol, '^[^:]+:', ''
                        )
                    )
                END,
                'multipleItemRequestId', request.pr_id,
                'timestamp', to_char(
                    request.pr_date_created,
                    'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                ),
                'requestingAgencyRequestId', request.import_requester_request_id,
                'supplyingAgencyRequestId', request.import_supplier_request_id
            )),
            'bibliographicInfo', jsonb_strip_nulls(jsonb_build_object(
                'supplierUniqueRecordId', request.pr_supplier_unique_record_id,
                'title', request.pr_title,
                'author', request.pr_author,
                'subtitle', request.pr_sub_title,
                'seriesTitle', request.pr_stitle,
                'edition', request.pr_edition,
                'titleOfComponent', request.pr_title_of_component,
                'authorOfComponent', request.pr_author_of_component,
                'volume', request.pr_volume,
                'issue', request.pr_issue,
                'pagesRequested', request.pr_pages_requested,
                'estimatedNoPages', request.pr_num_pages,
                'sponsor', request.pr_sponsor,
                'informationSource', request.pr_information_source,
                'bibliographicItemId',
                    CASE WHEN nullif(btrim(request.pr_isbn), '') IS NOT NULL
                        THEN jsonb_build_array(jsonb_build_object(
                            'bibliographicItemIdentifierCode',
                                jsonb_build_object('#text', 'isbn'),
                            'bibliographicItemIdentifier', request.pr_isbn
                        )) ELSE '[]'::jsonb END
                    ||
                    CASE WHEN nullif(btrim(request.pr_issn), '') IS NOT NULL
                        THEN jsonb_build_array(jsonb_build_object(
                            'bibliographicItemIdentifierCode',
                                jsonb_build_object('#text', 'issn'),
                            'bibliographicItemIdentifier', request.pr_issn
                        )) ELSE '[]'::jsonb END
            )),
            'publicationInfo', jsonb_strip_nulls(jsonb_build_object(
                'publisher', request.pr_publisher,
                'publicationType', CASE
                    WHEN request.import_publication_type IS NULL THEN NULL
                    ELSE jsonb_build_object(
                        '#text', request.import_publication_type
                    )
                END,
                'publicationDate', request.pr_pub_date,
                'placeOfPublication', request.pr_place_of_pub
            )),
            'serviceInfo', jsonb_strip_nulls(jsonb_build_object(
                'serviceType', request.import_service_type,
                'serviceLevel', CASE
                    WHEN request.import_service_level IS NULL THEN NULL
                    ELSE jsonb_build_object('#text', request.import_service_level)
                END,
                'copyrightCompliance', CASE
                    WHEN request.import_copyright_type IS NULL THEN NULL
                    ELSE jsonb_build_object('#text', request.import_copyright_type)
                END,
                'anyEdition', 'Y',
                'note', request.pr_patron_note
            )),
            'patronInfo', jsonb_strip_nulls(jsonb_build_object(
                'patronId', request.pr_patron_identifier,
                'surname', request.pr_patron_surname,
                'givenName', request.pr_patron_name,
                'patronType', CASE
                    WHEN request.pr_patron_type IS NULL THEN NULL
                    ELSE jsonb_build_object('#text', request.pr_patron_type)
                END,
                'sendToPatron', CASE
                    WHEN request.pr_send_to_patron IS TRUE THEN 'Y'
                    WHEN request.pr_send_to_patron IS FALSE THEN 'N'
                END
            )),
            'billingInfo', jsonb_strip_nulls(jsonb_build_object(
                'maximumCosts', CASE
                    WHEN request.pr_maximum_costs_value IS NULL
                      OR request.import_maximum_cost_currency IS NULL THEN NULL
                    ELSE jsonb_build_object(
                        'currencyCode', jsonb_build_object(
                            '#text', request.import_maximum_cost_currency
                        ),
                        'monetaryValue', request.pr_maximum_costs_value
                    )
                END
            ))
        )) AS ill_request
    FROM crosslink_open_requests AS request
),
request_bundles AS (
    SELECT
        request.import_patron_request_id AS pr_id,
        jsonb_strip_nulls(jsonb_build_object(
            'patronRequest', jsonb_strip_nulls(jsonb_build_object(
                'id', request.import_patron_request_id,
                'createdAt', to_char(
                    request.pr_date_created,
                    'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                ),
                'updatedAt', to_char(
                    request.pr_last_updated,
                    'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                ),
                'illRequest', request.ill_request,
                'state', request.import_state,
                'stateModel', 'default',
                'side', request.import_side,
                'patron', request.pr_patron_identifier,
                'requesterSymbol', request.pr_req_inst_symbol,
                'supplierSymbol', request.pr_sup_inst_symbol,
                'requesterRequestId', request.import_requester_request_id,
                'needsAttention', request.import_needs_attention,
                'internalNote', request.pr_local_note,
                'nextReqId', request.import_next_request_id,
                'prevReqId', request.import_previous_request_id
            )),
            'items', CASE
                WHEN nullif(btrim(request.pr_selected_item_barcode), '') IS NULL
                    THEN '[]'::jsonb
                ELSE jsonb_build_array(jsonb_strip_nulls(jsonb_build_object(
                    'id', request.pr_id || ':selected-item',
                    'barcode', request.pr_selected_item_barcode,
                    'callNumber', request.pr_local_call_number,
                    'title', request.pr_title,
                    'itemId', request.pr_system_instance_id,
                    'lmsRequestId', request.pr_external_hold_request_id,
                    'createdAt', to_char(
                        request.pr_date_created,
                        'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                    )
                )))
            END,
            'notifications', coalesce((
                SELECT jsonb_agg(notification.record ORDER BY notification.created_at)
                FROM (
                    SELECT
                        coalesce(note.prn_timestamp, note.prn_date_created)
                            AS created_at,
                        jsonb_strip_nulls(jsonb_build_object(
                            'id', note.prn_id,
                            'fromSymbol', CASE
                                WHEN note.prn_is_sender IS TRUE THEN :'owner'
                                ELSE coalesce(note.prn_sender_symbol, counterpart.symbol)
                            END,
                            'toSymbol', CASE
                                WHEN note.prn_is_sender IS TRUE THEN counterpart.symbol
                                ELSE :'owner'
                            END,
                            'direction', CASE
                                WHEN note.prn_is_sender IS TRUE THEN 'sent'
                                ELSE 'received'
                            END,
                            'kind', 'note',
                            'note', coalesce(
                                note.prn_message_content,
                                note.prn_action_data,
                                note.prn_action_status,
                                note.prn_attached_action
                            ),
                            'receipt', CASE upper(note.prn_message_status)
                                WHEN 'ACCEPTED' THEN 'ACCEPTED'
                                WHEN 'REJECTED' THEN 'REJECTED'
                                WHEN 'SEEN' THEN 'SEEN'
                                WHEN 'SENT' THEN 'SENT'
                                WHEN 'FAILED_TO_SEND' THEN 'FAILED_TO_SEND'
                            END,
                            'createdAt', to_char(
                                coalesce(note.prn_timestamp, note.prn_date_created),
                                'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                            ),
                            'acknowledgedAt', CASE
                                WHEN note.prn_seen IS TRUE THEN to_char(
                                    note.prn_last_updated,
                                    'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                                )
                            END
                        )) AS record
                    FROM patron_request_notification AS note
                    CROSS JOIN LATERAL (
                        SELECT CASE
                            WHEN request.import_side = 'borrowing'
                                THEN request.pr_sup_inst_symbol
                            ELSE request.pr_req_inst_symbol
                        END AS symbol
                    ) AS counterpart
                    WHERE note.prn_patron_request_fk = request.pr_id
                      AND coalesce(note.prn_timestamp, note.prn_date_created)
                          IS NOT NULL
                      AND (
                          (note.prn_is_sender IS TRUE
                           AND nullif(btrim(counterpart.symbol), '') IS NOT NULL)
                          OR
                          (note.prn_is_sender IS NOT TRUE
                           AND nullif(btrim(coalesce(
                               note.prn_sender_symbol,
                               counterpart.symbol
                           )), '') IS NOT NULL)
                      )

                    UNION ALL

                    SELECT
                        condition.prlc_date_created AS created_at,
                        jsonb_strip_nulls(jsonb_build_object(
                            'id', condition.prlc_id,
                            'fromSymbol', CASE
                                WHEN request.import_side = 'borrowing'
                                    THEN coalesce(
                                        condition.prlc_sup_inst_symbol,
                                        request.pr_sup_inst_symbol
                                    )
                                ELSE :'owner'
                            END,
                            'toSymbol', CASE
                                WHEN request.import_side = 'borrowing'
                                    THEN :'owner'
                                ELSE request.pr_req_inst_symbol
                            END,
                            'direction', CASE
                                WHEN request.import_side = 'borrowing'
                                    THEN 'received'
                                ELSE 'sent'
                            END,
                            'kind', 'condition',
                            'note', condition.prlc_note,
                            'cost', condition.prlc_cost,
                            'currency', condition_currency.rdv_value,
                            'condition', condition.prlc_code,
                            'createdAt', to_char(
                                condition.prlc_date_created,
                                'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                            )
                        )) AS record
                    FROM patron_request_loan_condition AS condition
                    LEFT JOIN refdata_value AS condition_currency
                      ON condition_currency.rdv_id = condition.prlc_cost_currency_fk
                    WHERE condition.prlc_patron_request_fk = request.pr_id
                      AND condition.prlc_date_created IS NOT NULL
                      AND nullif(btrim(CASE
                          WHEN request.import_side = 'borrowing'
                              THEN coalesce(
                                  condition.prlc_sup_inst_symbol,
                                  request.pr_sup_inst_symbol
                              )
                          ELSE :'owner'
                      END), '') IS NOT NULL
                ) AS notification
            ), '[]'::jsonb),
            'illTransaction', CASE
                WHEN request.import_side <> 'borrowing' THEN NULL
                ELSE jsonb_strip_nulls(jsonb_build_object(
                    'id', request.pr_id || ':ill',
                    'timestamp', to_char(
                        request.pr_date_created,
                        'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
                    ),
                    'requesterSymbol', request.pr_req_inst_symbol,
                    'supplierSymbol', request.pr_sup_inst_symbol,
                    'requesterRequestID', request.import_requester_request_id,
                    'supplierRequestID', request.pr_peer_request_identifier,
                    'illTransactionData', jsonb_strip_nulls(jsonb_build_object(
                        'bibliographicInfo', request.ill_request -> 'bibliographicInfo',
                        'publicationInfo', request.ill_request -> 'publicationInfo',
                        'serviceInfo', request.ill_request -> 'serviceInfo',
                        'patronInfo', request.ill_request -> 'patronInfo',
                        'billingInfo', request.ill_request -> 'billingInfo'
                    ))
                ))
            END,
            'locatedSuppliers', CASE
                WHEN request.import_side <> 'borrowing' THEN '[]'::jsonb
                ELSE coalesce((
                    SELECT jsonb_agg(
                        jsonb_strip_nulls(jsonb_build_object(
                            'id', rota.prr_id,
                            'supplierSymbol', authority.na_symbol || ':' || symbol.sym_symbol,
                            'ordinal', rota.prr_rota_position::integer,
                            'supplierStatus', CASE
                                WHEN request.pr_rota_position IS NULL
                                  OR rota.prr_rota_position > request.pr_rota_position
                                    THEN 'new'
                                WHEN rota.prr_rota_position = request.pr_rota_position
                                    THEN 'selected'
                                ELSE 'skipped'
                            END,
                            'lastStatus', rota_status.st_code,
                            'localID', rota.prr_system_identifier,
                            'lastReason', rota.prr_note,
                            'supplierRequestID', CASE
                                WHEN rota.prr_rota_position = request.pr_rota_position
                                    THEN request.pr_peer_request_identifier
                            END,
                            'localSupplier',
                                authority.na_symbol || ':' || symbol.sym_symbol = :'owner'
                        )) ORDER BY rota.prr_rota_position
                    )
                    FROM patron_request_rota AS rota
                    JOIN symbol
                      ON symbol.sym_id = rota.prr_peer_symbol_fk
                    JOIN naming_authority AS authority
                      ON authority.na_id = symbol.sym_authority_fk
                    LEFT JOIN status AS rota_status
                      ON rota_status.st_id = rota.prr_state_fk
                    WHERE rota.prr_patron_request_fk = request.pr_id
                ), '[]'::jsonb)
            END
        )) AS bundle
    FROM request_payloads AS request
)
SELECT jsonb_build_object(
    'type', 'patronRequest',
    'owner', :'owner',
    'data', request_bundles.bundle
)::text
FROM request_bundles
ORDER BY request_bundles.pr_id;

ROLLBACK;
