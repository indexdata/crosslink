#!/bin/sh
set -eu

deck_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
template="$deck_root/wolfcon/WOLFcon-2026-PowerPoint-Template.pptx"
source_md="$deck_root/wolfcon/WolfCON-2026-ReShare.md"
output="$deck_root/wolfcon/WolfCON-2026-ReShare.pptx"
shape_id_fixer="$deck_root/wolfcon/fix-pptx-shape-ids.py"
title_logo_inserter="$deck_root/wolfcon/insert-title-logo.py"
reference_dir=$(mktemp -d /private/tmp/wolfcon-reference.XXXXXX)
reference_pptx="$reference_dir/WOLFcon-2026-reference-for-pandoc.pptx"
source_for_pptx="$reference_dir/WolfCON-2026-ReShare.md"
generated_pptx="$reference_dir/WolfCON-2026-ReShare-generated.pptx"
fixed_pptx="$reference_dir/WolfCON-2026-ReShare-fixed.pptx"
package_dir="$reference_dir/package"

cleanup() {
  rm -rf "$reference_dir"
}
trap cleanup EXIT INT TERM

unzip -q "$template" -d "$reference_dir/template"

# Pandoc selects layouts by conventional names. Prefer the template's branded
# white content layout and illustrated title layout over its dark alternates.
perl -pi -e 's/name="Title and Bullets"/name="Title and Content"/' \
  "$reference_dir/template/ppt/slideLayouts/slideLayout1.xml"
perl -pi -e 's/name="Title Slide"/name="Dark Title Slide"/' \
  "$reference_dir/template/ppt/slideLayouts/slideLayout5.xml"
perl -pi -e 's/name="Title and Content"/name="Dark Title and Content"/' \
  "$reference_dir/template/ppt/slideLayouts/slideLayout6.xml"

# Pandoc is not reliable with the template's two-master relationship graph.
# Normalize the reference to one master before using it, while retaining every
# branded layout and attaching those layouts to the primary master.
python3 "$shape_id_fixer" "$reference_dir/template"

(cd "$reference_dir/template" && zip -qr "$reference_pptx" .)

# PowerPoint is more reliable with raster images than direct SVG relationships.
# Keep SVGs in the shared source for the PDF, but use PNG renderings in PPTX.
rsvg-convert -w 1600 \
  -o "$reference_dir/wolfcon-2026-state-model-anatomy.png" \
  "$deck_root/wolfcon/wolfcon-2026-state-model-anatomy.svg"
rsvg-convert -w 1800 \
  -o "$reference_dir/wolfcon-2026-borrowing-flow.png" \
  "$deck_root/wolfcon/wolfcon-2026-borrowing-flow.svg"
rsvg-convert -w 1800 \
  -o "$reference_dir/wolfcon-2026-migration.png" \
  "$deck_root/wolfcon/wolfcon-2026-migration.svg"
rsvg-convert -w 1800 \
  -o "$reference_dir/wolfcon-2026-migration-validation.png" \
  "$deck_root/wolfcon/wolfcon-2026-migration-validation.svg"
rsvg-convert -w 1800 \
  -o "$reference_dir/wolfcon-2026-migration-cutover.png" \
  "$deck_root/wolfcon/wolfcon-2026-migration-cutover.svg"
rsvg-convert -w 1800 \
  -o "$reference_dir/wolfcon-2026-durable-events.png" \
  "$deck_root/wolfcon/wolfcon-2026-durable-events.svg"
rsvg-convert -w 1800 \
  -o "$reference_dir/wolfcon-2026-model-api-ui.png" \
  "$deck_root/wolfcon/wolfcon-2026-model-api-ui.svg"

cp "$source_md" "$source_for_pptx"
perl -pi -e 's/wolfcon-2026-state-model-anatomy\.svg/wolfcon-2026-state-model-anatomy.png/g' "$source_for_pptx"
perl -pi -e 's/wolfcon-2026-borrowing-flow\.svg/wolfcon-2026-borrowing-flow.png/g' "$source_for_pptx"
perl -pi -e 's/wolfcon-2026-migration\.svg/wolfcon-2026-migration.png/g' "$source_for_pptx"
perl -pi -e 's/wolfcon-2026-migration-validation\.svg/wolfcon-2026-migration-validation.png/g' "$source_for_pptx"
perl -pi -e 's/wolfcon-2026-migration-cutover\.svg/wolfcon-2026-migration-cutover.png/g' "$source_for_pptx"
perl -pi -e 's/wolfcon-2026-durable-events\.svg/wolfcon-2026-durable-events.png/g' "$source_for_pptx"
perl -pi -e 's/wolfcon-2026-model-api-ui\.svg/wolfcon-2026-model-api-ui.png/g' "$source_for_pptx"

pandoc "$source_for_pptx" \
  --from=markdown \
  --to=pptx \
  --slide-level=1 \
  --reference-doc="$reference_pptx" \
  --resource-path="$reference_dir:$deck_root/wolfcon:$deck_root/misc:$deck_root" \
  --output="$generated_pptx"

# Pandoc can reuse cNvPr id="1" when it inserts images into a reference
# template. PowerPoint considers those drawing IDs invalid and requests repair.
unzip -q "$generated_pptx" -d "$package_dir"

# The official title-slide logo is a slide-level object in the template, so
# Pandoc does not copy it with the title layout. Restore it after generation.
cp "$reference_dir/template/ppt/media/image3.png" \
  "$package_dir/ppt/media/wolfcon-2026-logo.png"
python3 "$title_logo_inserter" "$package_dir"

python3 "$shape_id_fixer" "$package_dir"

if find "$package_dir/ppt/media" -type f -name '*.svg' | grep -q .; then
  echo "PPTX compatibility check failed: SVG media remains" >&2
  exit 1
fi

(cd "$package_dir" && zip -qr "$fixed_pptx" .)
mv "$fixed_pptx" "$output"

unzip -t "$output" >/dev/null
printf 'Generated %s\n' "$output"
