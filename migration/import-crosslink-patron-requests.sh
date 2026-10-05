#!/bin/sh

set -eu
umask 077

: "${CROSSLINK_BROKER_HOST:?CROSSLINK_BROKER_HOST must be set, for example http://localhost:8081}"

PATRON_REQUEST_FILE=${PATRON_REQUEST_FILE:-patron-requests.ndjson}
CONFLICT_POLICY=${CONFLICT_POLICY:-fail}
CROSSLINK_BASE_URL=${CROSSLINK_BROKER_HOST%/}

if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required to inspect the patron-request import response" >&2
    exit 1
fi

if [ ! -f "$PATRON_REQUEST_FILE" ]; then
    echo "Patron request export file not found: $PATRON_REQUEST_FILE" >&2
    exit 1
fi

response_file=$(mktemp "${TMPDIR:-/tmp}/crosslink-patron-request-import.XXXXXX")
trap 'rm -f "$response_file"' EXIT HUP INT TERM

curl_status=0
curl --fail-with-body \
    -X POST \
    -H 'Content-Type: application/x-ndjson' \
    --data-binary "@$PATRON_REQUEST_FILE" \
    "$CROSSLINK_BASE_URL/import?conflictPolicy=$CONFLICT_POLICY" \
    >"$response_file" || curl_status=$?

cat "$response_file"

if [ "$curl_status" -ne 0 ]; then
    exit "$curl_status"
fi

failed=$(jq -er '
    . as $response
    | ([
          (.patronRequests.failed // 0),
          (.batchActions.failed // 0),
          (.templates.failed // 0)
      ] | any(. != 0))
    or any($response.errors[]?; .type == null)
    | if . then "true" else "false" end
' "$response_file") || {
    echo "Patron-request import response is not valid JSON" >&2
    exit 1
}

if [ "$failed" = true ]; then
    echo "Patron-request import reported failed records" >&2
    exit 1
fi
