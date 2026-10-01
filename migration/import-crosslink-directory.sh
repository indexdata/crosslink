#!/bin/sh

set -eu
umask 077

: "${CROSSLINK_HOST:?CROSSLINK_HOST must be set, for example http://localhost:8086}"

DIRECTORY_FILE=${DIRECTORY_FILE:-directories.ndjson}
CONFLICT_POLICY=${CONFLICT_POLICY:-fail}
CROSSLINK_BASE_URL=${CROSSLINK_HOST%/}

if [ ! -f "$DIRECTORY_FILE" ]; then
    echo "Directory export file not found: $DIRECTORY_FILE" >&2
    exit 1
fi

curl --fail-with-body \
    -X POST \
    -H 'Content-Type: application/x-ndjson' \
    -H 'X-Okapi-Permissions: ["directory.consortium.all"]' \
    --data-binary "@$DIRECTORY_FILE" \
    "$CROSSLINK_BASE_URL/directory/import?conflictPolicy=$CONFLICT_POLICY"
