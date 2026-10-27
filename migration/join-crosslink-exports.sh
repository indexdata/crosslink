#!/bin/sh

set -eu
umask 077

SOURCE_DIR=${1:-.}
DIRECTORY_OUTPUT=${DIRECTORY_OUTPUT:-"$SOURCE_DIR/directories.ndjson"}
DIRECTORY_PREFIX=${DIRECTORY_PREFIX:-directories}
CONFIG_OUTPUT=${CONFIG_OUTPUT:-"$SOURCE_DIR/config.ndjson"}
CONFIG_PREFIX=${CONFIG_PREFIX:-config}
UNSUPPORTED_TEMPLATE_OUTPUT=${UNSUPPORTED_TEMPLATE_OUTPUT:-"$SOURCE_DIR/unsupported-templates.ndjson"}
UNSUPPORTED_TEMPLATE_PREFIX=${UNSUPPORTED_TEMPLATE_PREFIX:-unsupported-templates}
PATRON_REQUEST_PREFIX=${PATRON_REQUEST_PREFIX:-patron-request}
PATRON_REQUEST_DIR=${PATRON_REQUEST_DIR:-"$SOURCE_DIR/patron-requests"}
PATRON_REQUEST_BATCH_SIZE=${PATRON_REQUEST_BATCH_SIZE:-1000}

case "$PATRON_REQUEST_BATCH_SIZE" in
    ''|*[!0-9]*|0)
        echo "PATRON_REQUEST_BATCH_SIZE must be a positive integer: $PATRON_REQUEST_BATCH_SIZE" >&2
        exit 1
        ;;
esac

if [ ! -d "$SOURCE_DIR" ]; then
    echo "Source directory not found: $SOURCE_DIR" >&2
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "jq is required to consolidate directory tiers and network members" >&2
    exit 1
fi

TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/crosslink-join.XXXXXX")
trap 'rm -rf "$TEMP_DIR"' EXIT HUP INT TERM

join_exports() {
    prefix=$1
    output=$2
    required=${3:-true}
    list_file="$TEMP_DIR/$prefix.list"
    sorted_list_file="$TEMP_DIR/$prefix.sorted.list"
    found=false

    : >"$list_file"
    for file in "$SOURCE_DIR"/"$prefix"-*.ndjson; do
        if [ ! -f "$file" ]; then
            continue
        fi

        filename=${file##*/}
        export_number=${filename##*-}
        export_number=${export_number%.ndjson}
        case "$export_number" in
            ''|*[!0-9]*)
                echo "Invalid export filename: $file" >&2
                exit 1
                ;;
        esac

        printf '%020d\t%s\n' "$export_number" "$file" >>"$list_file"
        found=true
    done

    if [ "$found" = false ]; then
        if [ "$required" = false ]; then
            : >"$output"
            chmod 600 "$output"
            printf '%s\n' "No $prefix export files found; wrote empty $output"
            return
        fi

        echo "No $prefix export files found in $SOURCE_DIR" >&2
        exit 1
    fi

    sort -n -k1,1 "$list_file" >"$sorted_list_file"

    : >"$output"
    chmod 600 "$output"

    if [ "$prefix" = "$DIRECTORY_PREFIX" ]; then
        all_records="$TEMP_DIR/$prefix.all.ndjson"
        entry_records="$TEMP_DIR/$prefix.entries.ndjson"
        metadata_records="$TEMP_DIR/$prefix.metadata.ndjson"

        : >"$all_records"
        while IFS='	' read -r _ file; do
            cat "$file" >>"$all_records"
        done <"$sorted_list_file"

        jq -c 'select(.type == "entry")' "$all_records" >"$entry_records"
        jq -c 'select(.type == "tier" or .type == "network")' "$all_records" >"$metadata_records"

        if [ ! -s "$metadata_records" ]; then
            echo "No tier or network records found in directory exports" >&2
            exit 1
        fi

        jq -n -c \
            --slurpfile entries "$entry_records" \
            --slurpfile metadata "$metadata_records" \
            '
                ($entries | map(select(.data.type == "Consortium") | .key) | first) as $consortium_key |
                ($entries | map(
                    if $consortium_key != null
                       and .data.type == "Institution"
                       and .data.parent == null
                    then
                        .data.parent = $consortium_key
                    else
                        .
                    end
                )) as $entries_with_parents |
                ($entries_with_parents | map(select(.data.type != "Consortium") | .key)) as $member_keys |
                ($member_keys | to_entries | map({entry: .value, priority: (.key + 1)})) as $network_members |
                (($entries_with_parents) + ($metadata | map(
                    if .type == "tier" then
                        .data.entries = $member_keys
                    elif .type == "network" then
                        .data.entries = $network_members
                    else
                        .
                    end
                )))[]
            ' >"$output"
        printf '%s\n' "Joined $prefix exports into $output"
    else
        while IFS='	' read -r _ file; do
            cat "$file" >>"$output"
        done <"$sorted_list_file"
    fi
}

# Splits the joined patron requests into numbered files of at most
# PATRON_REQUEST_BATCH_SIZE records. Each NDJSON record is one line.
split_patron_requests() {
    input=$1

    mkdir -p "$PATRON_REQUEST_DIR"
    for file in "$PATRON_REQUEST_DIR"/patron-requests-*.ndjson; do
        if [ -f "$file" ]; then
            rm "$file"
        fi
    done

    files=$(awk -v dir="$PATRON_REQUEST_DIR" -v size="$PATRON_REQUEST_BATCH_SIZE" '
        (NR - 1) % size == 0 {
            if (out != "") close(out)
            batch++
            out = sprintf("%s/patron-requests-%04d.ndjson", dir, batch)
        }
        { print > out }
        END { print batch + 0 }
    ' "$input")
    records=$(wc -l <"$input" | tr -d ' ')

    printf '%s\n' "Wrote $records patron requests to $files file(s) in $PATRON_REQUEST_DIR"
}

join_exports "$DIRECTORY_PREFIX" "$DIRECTORY_OUTPUT"
join_exports "$CONFIG_PREFIX" "$CONFIG_OUTPUT"
join_exports "$UNSUPPORTED_TEMPLATE_PREFIX" "$UNSUPPORTED_TEMPLATE_OUTPUT" false
join_exports "$PATRON_REQUEST_PREFIX" "$TEMP_DIR/patron-requests.ndjson"
split_patron_requests "$TEMP_DIR/patron-requests.ndjson"
