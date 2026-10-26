#!/bin/sh

set -eu
umask 077

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
EXPORT_WORKING_DIR=$(pwd)
PROPERTIES_FILE=${PROPERTIES_FILE:-"$SCRIPT_DIR/schema.properties"}
DIRECTORY_EXPORT=${DIRECTORY_EXPORT:-"$SCRIPT_DIR/export-crosslink-directory.sql"}
CONFIG_EXPORT=${CONFIG_EXPORT:-"$SCRIPT_DIR/export-crosslink-config.sql"}
PATRON_REQUEST_EXPORT=${PATRON_REQUEST_EXPORT:-"$SCRIPT_DIR/export-crosslink-open-patron-requests.sql"}
DIRECTORY_OUTPUT=${DIRECTORY_OUTPUT:-directories.ndjson}
DIRECTORY_PREFIX=${DIRECTORY_PREFIX:-directories}
CONFIG_PREFIX=${CONFIG_PREFIX:-config}
CONFIG_OUTPUT=${CONFIG_OUTPUT:-config.ndjson}
UNSUPPORTED_TEMPLATE_PREFIX=${UNSUPPORTED_TEMPLATE_PREFIX:-unsupported-templates}
UNSUPPORTED_TEMPLATE_OUTPUT=${UNSUPPORTED_TEMPLATE_OUTPUT:-unsupported-templates.ndjson}
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

if [ ! -f "$CONFIG_EXPORT" ]; then
    echo "Config export SQL file not found: $CONFIG_EXPORT" >&2
    exit 1
fi

if [ ! -f "$PATRON_REQUEST_EXPORT" ]; then
    echo "Patron request export SQL file not found: $PATRON_REQUEST_EXPORT" >&2
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required to reuse the consortium UUID across directory exports" >&2
    exit 1
fi

for file in "$DIRECTORY_PREFIX"-*.ndjson "$CONFIG_PREFIX"-*.ndjson \
    "$UNSUPPORTED_TEMPLATE_PREFIX"-*.ndjson \
    "$PATRON_REQUEST_PREFIX"-*.ndjson; do
    if [ -f "$file" ]; then
        rm "$file"
    fi
done

total_entries=$(awk '
    /^[[:space:]]*($|#)/ { next }
    { count++ }
    END { print count + 0 }
' "$PROPERTIES_FILE")

index=0
consortium_uuid=

while IFS= read -r line || [ -n "$line" ]; do
    trimmed_line=${line#"${line%%[![:space:]]*}"}
    case "$trimmed_line" in
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

    index=$((index + 1))
    include_consortium=false
    include_tiers_network=false
    if [ "$index" -eq 1 ]; then
        include_consortium=true
    fi
    if [ "$index" -eq "$total_entries" ]; then
        include_tiers_network=true
    fi

    directory_output=${DIRECTORY_PREFIX}-${schema}-${index}.ndjson
    printf '%s\n' "Exporting directory for schema $schema to $directory_output"
    psql "$DATABASE_URL" \
        --command="SET search_path TO \"$schema\";" \
        --set="owner=$owner_symbol" \
        --set="include_consortium=$include_consortium" \
        --set="include_tiers_network=$include_tiers_network" \
        --set="consortium=$consortium_uuid" \
        --set=ON_ERROR_STOP=1 \
        --file="$DIRECTORY_EXPORT" \
        --quiet --tuples-only --no-align \
        >"$directory_output"

    if [ "$index" -eq 1 ]; then
        consortium_uuid=$(jq -s -er '
            [.[] | select(.type == "entry" and .data.type == "Consortium")]
            | if length == 1 then .[0].key
              else error("first directory export must contain exactly one consortium entry")
              end
        ' "$directory_output")
    fi

    config_output=${CONFIG_PREFIX}-${schema}-${index}.ndjson
    unsupported_template_output=${UNSUPPORTED_TEMPLATE_PREFIX}-${schema}-${index}.ndjson
    printf '%s\n' "Exporting config for schema $schema to $config_output"
    psql "$DATABASE_URL" \
        --command="SET search_path TO \"$schema\";" \
        --set="owner=$owner_symbol" \
        --set="unsupported_template_output=$unsupported_template_output" \
        --set=ON_ERROR_STOP=1 \
        --file="$CONFIG_EXPORT" \
        --quiet --tuples-only --no-align \
        >"$config_output"

    case "$config_output" in
        /*) config_output_path=$config_output ;;
        *) config_output_path=$EXPORT_WORKING_DIR/$config_output ;;
    esac
    case "$unsupported_template_output" in
        /*) unsupported_template_output_path=$unsupported_template_output ;;
        *) unsupported_template_output_path=$EXPORT_WORKING_DIR/$unsupported_template_output ;;
    esac
    (cd "$SCRIPT_DIR/../broker" && go run ./cmd/migration-template-validator \
        "$config_output_path" "$unsupported_template_output_path")

    patron_output=${PATRON_REQUEST_PREFIX}-${schema}-${index}.ndjson
    printf '%s\n' "Exporting patron requests for schema $schema to $patron_output"
    psql "$DATABASE_URL" \
        --command="SET search_path TO \"$schema\";" \
        --set=ON_ERROR_STOP=1 \
        --set="owner=$owner_symbol" \
        --set=broker=ISIL:BROKER \
        --file="$PATRON_REQUEST_EXPORT" \
        --quiet --tuples-only --no-align \
        >"$patron_output"
done <"$PROPERTIES_FILE"

if [ "$index" -eq 0 ]; then
    echo "No schema entries found in $PROPERTIES_FILE" >&2
    exit 1
fi

DIRECTORY_OUTPUT="$DIRECTORY_OUTPUT" \
DIRECTORY_PREFIX="$DIRECTORY_PREFIX" \
CONFIG_OUTPUT="$CONFIG_OUTPUT" \
CONFIG_PREFIX="$CONFIG_PREFIX" \
UNSUPPORTED_TEMPLATE_OUTPUT="$UNSUPPORTED_TEMPLATE_OUTPUT" \
UNSUPPORTED_TEMPLATE_PREFIX="$UNSUPPORTED_TEMPLATE_PREFIX" \
PATRON_REQUEST_PREFIX="$PATRON_REQUEST_PREFIX" \
"$SCRIPT_DIR/join-crosslink-exports.sh" .
