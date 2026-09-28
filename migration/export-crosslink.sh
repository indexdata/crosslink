#!/bin/sh

set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROPERTIES_FILE=${PROPERTIES_FILE:-"$SCRIPT_DIR/schema.properties"}
DIRECTORY_EXPORT=${DIRECTORY_EXPORT:-"$SCRIPT_DIR/export-crosslink-directory.sql"}
PATRON_REQUEST_EXPORT=${PATRON_REQUEST_EXPORT:-"$SCRIPT_DIR/export-crosslink-open-patron-requests.sql"}
DIRECTORY_OUTPUT=${DIRECTORY_OUTPUT:-directories.ndjson}
PATRON_REQUEST_PREFIX=${PATRON_REQUEST_PREFIX:-patron-request}

: "${DATABASE_URL:?DATABASE_URL must be set}"

if [ ! -f "$PROPERTIES_FILE" ]; then
    echo "Properties file not found: $PROPERTIES_FILE" >&2
    exit 1
fi

if [ ! -f "$DIRECTORY_EXPORT" ]; then
    echo "Directory export SQL file not found: $DIRECTORY_EXPORT" >&2
    exit 1
fi

if [ ! -f "$PATRON_REQUEST_EXPORT" ]; then
    echo "Patron request export SQL file not found: $PATRON_REQUEST_EXPORT" >&2
    exit 1
fi

first_schema=
index=0

while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in
        ''|'#'*)
            continue
            ;;
    esac

    case "$line" in
        *=*)
            schema=${line%%=*}
            owner_symbol=${line#*=}
            ;;
        *)
            echo "Invalid properties line (expected schema=ownerSymbol): $line" >&2
            exit 1
            ;;
    esac

    case "$schema" in
        ''|*[!A-Za-z0-9_]*|[0-9]*)
            echo "Invalid schema identifier: $schema" >&2
            exit 1
            ;;
    esac

    case "$owner_symbol" in
        *:*)
            owner_authority=${owner_symbol%%:*}
            owner_value=${owner_symbol#*:}
            ;;
        *)
            echo "Invalid owner symbol (expected AUTHORITY:VALUE): $owner_symbol" >&2
            exit 1
            ;;
    esac

    if [ -z "$owner_authority" ] || [ -z "$owner_value" ]; then
        echo "Invalid owner symbol (expected AUTHORITY:VALUE): $owner_symbol" >&2
        exit 1
    fi

    if [ -z "$first_schema" ]; then
        first_schema=$schema
        printf '%s\n' "Exporting directory for schema $schema to $DIRECTORY_OUTPUT"
        psql "$DATABASE_URL" \
            --command="SET search_path TO $schema;" \
            --set=ON_ERROR_STOP=1 \
            --file="$DIRECTORY_EXPORT" \
            --quiet --tuples-only --no-align \
            >"$DIRECTORY_OUTPUT"
    fi

    index=$((index + 1))
    patron_output=${PATRON_REQUEST_PREFIX}-${schema}-${index}.ndjson
    printf '%s\n' "Exporting patron requests for schema $schema to $patron_output"
    psql "$DATABASE_URL" \
        --command="SET search_path TO $schema;" \
        --set=ON_ERROR_STOP=1 \
        --set="owner=$owner_symbol" \
        --set=broker=ISIL:BROKER \
        --file="$PATRON_REQUEST_EXPORT" \
        --quiet --tuples-only --no-align \
        >"$patron_output"
done <"$PROPERTIES_FILE"

if [ -z "$first_schema" ]; then
    echo "No schema entries found in $PROPERTIES_FILE" >&2
    exit 1
fi
