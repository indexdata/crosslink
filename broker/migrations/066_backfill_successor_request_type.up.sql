-- Before rerequest was introduced, every borrowing successor was a retry.
-- The old builder copied requestType from the original request (often New or
-- absent). Record the protocol intent before the new code starts sending or
-- creating successors. No predecessor row or state is needed.
UPDATE patron_request
SET ill_request = jsonb_set(
        COALESCE(NULLIF(ill_request, 'null'::jsonb), '{}'::jsonb),
        '{serviceInfo}',
        COALESCE(NULLIF(ill_request -> 'serviceInfo', 'null'::jsonb),
                 '{"serviceType":"Loan"}'::jsonb)
            || jsonb_build_object('requestType', 'Retry',
                                  'requestingAgencyPreviousRequestId', prev_req_id)
    ),
    updated_at = now()
WHERE side = 'borrowing'
  AND prev_req_id IS NOT NULL;
