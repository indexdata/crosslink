#!/bin/sh

set -eu
umask 077

: "${CROSSLINK_BROKER_HOST:?CROSSLINK_BROKER_HOST must be set, for example http://localhost:8081}"

PATRON_REQUEST_FILE=${PATRON_REQUEST_FILE:-patron-requests.ndjson}
CONFLICT_POLICY=${CONFLICT_POLICY:-fail}
CROSSLINK_BASE_URL=${CROSSLINK_BROKER_HOST%/}

if [ ! -f "$PATRON_REQUEST_FILE" ]; then
    echo "Patron request export file not found: $PATRON_REQUEST_FILE" >&2
    exit 1
fi

curl --fail-with-body \
    -X POST \
    -H 'Content-Type: application/x-ndjson' \
    --data-binary "@$PATRON_REQUEST_FILE" \
    "$CROSSLINK_BASE_URL/import?conflictPolicy=$CONFLICT_POLICY"
