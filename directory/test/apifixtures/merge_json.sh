#!/bin/bash
json_array="[]"
for file in "$@"; do
    json_content=$(jq -c . "$file")
    json_array=$(echo "$json_array" | jq --argjson new_item "$json_content" '. + [$new_item]')
done

echo "$json_array" | jq .
