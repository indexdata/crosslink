#!/bin/sh

set -eu
umask 077

: "${CROSSLINK_HOST:?CROSSLINK_HOST must be set, for example http://localhost:8086}"

DIRECTORY_FILE=${DIRECTORY_FILE:-directories.ndjson}
CONFLICT_POLICY=${CONFLICT_POLICY:-fail}
CROSSLINK_BASE_URL=${CROSSLINK_HOST%/}

if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required to inspect the directory import response" >&2
    exit 1
fi

if [ ! -f "$DIRECTORY_FILE" ]; then
    echo "Directory export file not found: $DIRECTORY_FILE" >&2
    exit 1
fi

response_file=$(mktemp "${TMPDIR:-/tmp}/crosslink-directory-import.XXXXXX")
trap 'rm -f "$response_file"' EXIT HUP INT TERM

curl_status=0
curl --fail-with-body \
    -X POST \
    -H 'Content-Type: application/x-ndjson' \
    -H 'X-Okapi-Permissions: ["directory.consortium.all"]' \
    --data-binary "@$DIRECTORY_FILE" \
    "$CROSSLINK_BASE_URL/directory/import?conflictPolicy=$CONFLICT_POLICY" \
    >"$response_file" || curl_status=$?

cat "$response_file"

if [ "$curl_status" -ne 0 ]; then
    exit "$curl_status"
fi

failed=$(jq -er '
    . as $response
    | ([
          (.entries.failed // 0),
          (.tiers.failed // 0),
          (.networks.failed // 0)
      ] | any(. != 0))
    or any($response.errors[]?; .type == null)
    | if . then "true" else "false" end
' "$response_file") || {
    echo "Directory import response is not valid JSON" >&2
    exit 1
}

if [ "$failed" = true ]; then
    echo "Directory import reported failed records" >&2
    exit 1
fi
