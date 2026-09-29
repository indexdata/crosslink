#!/bin/sh

set -eu

SOURCE_DIR=${1:-.}
DIRECTORY_OUTPUT=${DIRECTORY_OUTPUT:-"$SOURCE_DIR/directories.ndjson"}
PATRON_REQUEST_OUTPUT=${PATRON_REQUEST_OUTPUT:-"$SOURCE_DIR/patron-requests.ndjson"}

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
        echo "No $prefix export files found in $SOURCE_DIR" >&2
        exit 1
    fi

    sort -n -k1,1 "$list_file" >"$sorted_list_file"

    if [ "$prefix" = directories ]; then
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
                ($entries | map(select(.data.type != "Consortium") | .key)) as $member_keys |
                ($member_keys | to_entries | map(.value + {priority: (.key + 1)})) as $network_members |
                (($entries) + ($metadata | map(
                    if .type == "tier" then
                        .data.entries = $member_keys
                    elif .type == "network" then
                        .data.entries = $network_members
                    else
                        .
                    end
                )))[]
            ' >"$output"
    else
        : >"$output"
        while IFS='	' read -r _ file; do
            cat "$file" >>"$output"
        done <"$sorted_list_file"
    fi

    printf '%s\n' "Joined $prefix exports into $output"
}

join_exports directories "$DIRECTORY_OUTPUT"
join_exports patron-request "$PATRON_REQUEST_OUTPUT"
