#!/bin/sh

set -eu

SOURCE_DIR=${1:-.}
DIRECTORY_OUTPUT=${DIRECTORY_OUTPUT:-"$SOURCE_DIR/directories.ndjson"}
PATRON_REQUEST_OUTPUT=${PATRON_REQUEST_OUTPUT:-"$SOURCE_DIR/patron-requests.ndjson"}

if [ ! -d "$SOURCE_DIR" ]; then
    echo "Source directory not found: $SOURCE_DIR" >&2
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
    : >"$output"
    while IFS='	' read -r _ file; do
        cat "$file" >>"$output"
    done <"$sorted_list_file"

    printf '%s\n' "Joined $prefix exports into $output"
}

join_exports directories "$DIRECTORY_OUTPUT"
join_exports patron-request "$PATRON_REQUEST_OUTPUT"
