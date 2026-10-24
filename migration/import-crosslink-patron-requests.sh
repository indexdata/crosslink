#!/bin/sh

set -eu
umask 077

: "${CROSSLINK_BROKER_HOST:?CROSSLINK_BROKER_HOST must be set, for example http://localhost:8081}"

CONFIG_FILE=${CONFIG_FILE:-config.ndjson}
PATRON_REQUEST_DIR=${PATRON_REQUEST_DIR:-patron-requests}
PATRON_REQUEST_FILE=${PATRON_REQUEST_FILE:-}
CONFLICT_POLICY=${CONFLICT_POLICY:-fail}
CROSSLINK_BASE_URL=${CROSSLINK_BROKER_HOST%/}

if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required to inspect the import response" >&2
    exit 1
fi

response_file=$(mktemp "${TMPDIR:-/tmp}/crosslink-patron-request-import.XXXXXX")
trap 'rm -f "$response_file"' EXIT HUP INT TERM

# Posts one NDJSON file and prints the response. Returns nonzero when the
# request fails or the response reports failed records.
import_file() {
    file=$1

    curl_status=0
    curl --fail-with-body \
        -X POST \
        -H 'Content-Type: application/x-ndjson' \
        --data-binary "@$file" \
        "$CROSSLINK_BASE_URL/import?conflictPolicy=$CONFLICT_POLICY" \
        >"$response_file" || curl_status=$?

    cat "$response_file"
    echo

    if [ "$curl_status" -ne 0 ]; then
        return "$curl_status"
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
        echo "Import response is not valid JSON" >&2
        return 1
    }

    if [ "$failed" = true ]; then
        echo "Import reported failed records" >&2
        return 1
    fi
}

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Combined config file not found: $CONFIG_FILE" >&2
    exit 1
fi

if [ -n "$PATRON_REQUEST_FILE" ]; then
    if [ ! -f "$PATRON_REQUEST_FILE" ]; then
        echo "Patron request export file not found: $PATRON_REQUEST_FILE" >&2
        exit 1
    fi
else
    if [ ! -d "$PATRON_REQUEST_DIR" ]; then
        echo "Patron request export directory not found: $PATRON_REQUEST_DIR" >&2
        exit 1
    fi

    total=0
    for file in "$PATRON_REQUEST_DIR"/patron-requests-*.ndjson; do
        if [ -f "$file" ]; then
            total=$((total + 1))
        fi
    done

    if [ "$total" -eq 0 ]; then
        echo "No patron-requests-*.ndjson files found in $PATRON_REQUEST_DIR" >&2
        exit 1
    fi
fi

printf '%s\n' "Importing combined config $CONFIG_FILE"
config_status=0
import_file "$CONFIG_FILE" || config_status=$?
if [ "$config_status" -ne 0 ]; then
    echo "Configuration import stopped at $CONFIG_FILE; no patron requests were imported." >&2
    exit "$config_status"
fi

if [ -n "$PATRON_REQUEST_FILE" ]; then
    printf '%s\n' "Importing $PATRON_REQUEST_FILE"
    import_file "$PATRON_REQUEST_FILE"
    exit 0
fi

count=0
for file in "$PATRON_REQUEST_DIR"/patron-requests-*.ndjson; do
    count=$((count + 1))
    printf '%s\n' "Importing $file ($count/$total)"
    import_status=0
    import_file "$file" || import_status=$?
    if [ "$import_status" -ne 0 ]; then
        if [ "$count" -gt 1 ]; then
            echo "Import stopped at $file; the $((count - 1)) earlier file(s) were imported." >&2
            echo "Fix the problem, then re-run with CONFLICT_POLICY=skip (or update) to continue." >&2
        else
            echo "Import stopped at $file." >&2
        fi
        exit "$import_status"
    fi
done

printf '%s\n' "Imported $total file(s) from $PATRON_REQUEST_DIR"
