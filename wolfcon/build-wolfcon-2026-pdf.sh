#!/bin/sh
set -eu

deck_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
source_md=${1:-"$deck_root/wolfcon/WolfCON-2026-ReShare.md"}
style="$deck_root/wolfcon/wolfcon-2026-pdf.css"
template="$deck_root/wolfcon/WOLFcon-2026-PowerPoint-Template.pptx"
output=${2:-"${source_md%.md}.pdf"}
render_dir=$(mktemp -d /private/tmp/wolfcon-pdf.XXXXXX)
html="$render_dir/$(basename "${source_md%.md}").html"
chrome_profile="$render_dir/chrome-profile"
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"

cleanup() {
  rm -rf "$render_dir"
}
trap cleanup EXIT INT TERM

pandoc "$source_md" \
  --to=html5 \
  --standalone \
  --section-divs \
  --slide-level=1 \
  --embed-resources \
  --css="$style" \
  --resource-path="$deck_root/wolfcon:$deck_root/misc:$deck_root" \
  --output="$html"

# Reuse the official white logo embedded in the conference template.
unzip -q -j "$template" ppt/media/image1.png -d "$render_dir"
perl -0pi -e 's#(<header id="title-block-header"[^>]*>)#$1<img class="title-logo" src="image1.png" alt="WolfCON 2026 logo">#' "$html"

"$chrome" \
  --headless=new \
  --disable-gpu \
  --no-sandbox \
  --allow-file-access-from-files \
  --no-pdf-header-footer \
  --user-data-dir="$chrome_profile" \
  --print-to-pdf="$output" \
  "file://$html"

SWIFT_MODULECACHE_PATH="$render_dir/swift-module-cache" \
CLANG_MODULE_CACHE_PATH="$render_dir/swift-module-cache" \
  /usr/bin/swift "$deck_root/wolfcon/set-pdf-metadata.swift" "$output"

test -s "$output"
printf 'Generated %s\n' "$output"
