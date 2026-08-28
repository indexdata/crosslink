#!/usr/bin/env python3
"""Repair compatibility defects in a Pandoc-generated PPTX package."""

from __future__ import annotations

import re
import sys
import xml.etree.ElementTree as ET
from pathlib import Path


CNVPR_ID = re.compile(
    rb'(<(?:[A-Za-z0-9_]+:)?cNvPr\b[^>]*?\bid=")(\d+)(")'
)
RELATIONSHIP = re.compile(rb"<Relationship\b[^>]*/>")
TARGET = re.compile(rb'\bTarget="([^"]+)"')
TARGET_MODE = re.compile(rb'\bTargetMode="([^"]+)"')
REL_NS = "http://schemas.openxmlformats.org/package/2006/relationships"
P_NS = "http://schemas.openxmlformats.org/presentationml/2006/main"
R_NS = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
A_NS = "http://schemas.openxmlformats.org/drawingml/2006/main"
A16_NS = "http://schemas.microsoft.com/office/drawing/2014/main"
P14_NS = "http://schemas.microsoft.com/office/powerpoint/2010/main"
P15_NS = "http://schemas.microsoft.com/office/powerpoint/2012/main"
MC_NS = "http://schemas.openxmlformats.org/markup-compatibility/2006"
SLIDE_LAYOUT_REL = R_NS + "/slideLayout"
SLIDE_MASTER_REL = R_NS + "/slideMaster"

ET.register_namespace("", REL_NS)
ET.register_namespace("p", P_NS)
ET.register_namespace("r", R_NS)
ET.register_namespace("a", A_NS)
ET.register_namespace("a16", A16_NS)
ET.register_namespace("p14", P14_NS)
ET.register_namespace("p15", P15_NS)
ET.register_namespace("mc", MC_NS)


def repair_slide(path: Path) -> int:
    data = path.read_bytes()
    numeric_ids = [int(match.group(2)) for match in CNVPR_ID.finditer(data)]
    if not numeric_ids:
        return 0

    used: set[int] = set()
    next_id = max(numeric_ids) + 1
    changes = 0

    def replace(match: re.Match[bytes]) -> bytes:
        nonlocal next_id, changes
        current_id = int(match.group(2))
        if current_id not in used:
            used.add(current_id)
            return match.group(0)

        while next_id in used:
            next_id += 1
        replacement_id = next_id
        used.add(replacement_id)
        next_id += 1
        changes += 1
        return match.group(1) + str(replacement_id).encode("ascii") + match.group(3)

    repaired = CNVPR_ID.sub(replace, data)
    repaired_ids = [int(match.group(2)) for match in CNVPR_ID.finditer(repaired)]
    if len(repaired_ids) != len(set(repaired_ids)):
        raise RuntimeError(f"duplicate cNvPr IDs remain in {path}")

    if changes:
        path.write_bytes(repaired)
    return changes


def collapse_to_single_master(package_dir: Path) -> int:
    """Attach every retained layout to master 1 and remove master 2.

    The WolfCON template contains two slide masters. Pandoc retains layouts from
    both but can omit the second master part, leaving a relationship graph that
    PowerPoint rejects. A single master keeps the branding while making the
    reference document deterministic for Pandoc.
    """

    masters_dir = package_dir / "ppt" / "slideMasters"
    layouts_dir = package_dir / "ppt" / "slideLayouts"
    master_path = masters_dir / "slideMaster1.xml"
    master_rels_path = masters_dir / "_rels" / "slideMaster1.xml.rels"
    presentation_path = package_dir / "ppt" / "presentation.xml"
    presentation_rels_path = package_dir / "ppt" / "_rels" / "presentation.xml.rels"

    if not master_path.exists() or not master_rels_path.exists():
        raise RuntimeError("the PPTX package has no primary slide master")

    # Remove master 2 from the presentation relationship graph.
    presentation_rels_tree = ET.parse(presentation_rels_path)
    presentation_rels_root = presentation_rels_tree.getroot()
    removed_master_rids: set[str] = set()
    for relationship in list(presentation_rels_root):
        if (
            relationship.attrib.get("Type") == SLIDE_MASTER_REL
            and relationship.attrib.get("Target", "").endswith("slideMaster2.xml")
        ):
            removed_master_rids.add(relationship.attrib["Id"])
            presentation_rels_root.remove(relationship)
    if removed_master_rids:
        presentation_rels_tree.write(
            presentation_rels_path, encoding="UTF-8", xml_declaration=True
        )

    presentation_tree = ET.parse(presentation_path)
    presentation_root = presentation_tree.getroot()
    master_list = presentation_root.find(f"{{{P_NS}}}sldMasterIdLst")
    if master_list is not None:
        for master_id in list(master_list):
            if master_id.attrib.get(f"{{{R_NS}}}id") in removed_master_rids:
                master_list.remove(master_id)
    presentation_tree.write(presentation_path, encoding="UTF-8", xml_declaration=True)

    # Keep the primary branded layouts plus the two secondary-master layouts
    # Pandoc needs for columns and captioned figures. The remaining unused
    # layouts carry master-specific placeholder metadata that PowerPoint cannot
    # safely reinterpret under master 1.
    retained_layout_numbers = {1, 2, 3, 4, 8, 12}
    all_layout_paths = sorted(
        layouts_dir.glob("slideLayout*.xml"),
        key=lambda path: int(re.search(r"\d+", path.stem).group()),
    )
    layout_paths: list[Path] = []
    removed_layout_names: set[str] = set()
    for layout_path in all_layout_paths:
        layout_number = int(re.search(r"\d+", layout_path.stem).group())
        if layout_number in retained_layout_numbers:
            layout_paths.append(layout_path)
            continue
        removed_layout_names.add(layout_path.name)
        layout_path.unlink()
        (layouts_dir / "_rels" / f"{layout_path.name}.rels").unlink(missing_ok=True)

    layout_fallbacks = {
        "slideLayout5.xml": "slideLayout2.xml",
        "slideLayout6.xml": "slideLayout1.xml",
        "slideLayout7.xml": "slideLayout1.xml",
        "slideLayout9.xml": "slideLayout8.xml",
        "slideLayout10.xml": "slideLayout1.xml",
        "slideLayout11.xml": "slideLayout4.xml",
        "slideLayout13.xml": "slideLayout12.xml",
    }
    for slide_rels_path in (package_dir / "ppt" / "slides" / "_rels").glob(
        "slide*.xml.rels"
    ):
        slide_rels_tree = ET.parse(slide_rels_path)
        changed = False
        for relationship in slide_rels_tree.getroot():
            if relationship.attrib.get("Type") != SLIDE_LAYOUT_REL:
                continue
            target_name = Path(relationship.attrib.get("Target", "")).name
            fallback_name = layout_fallbacks.get(target_name)
            if fallback_name is not None:
                relationship.set("Target", f"../slideLayouts/{fallback_name}")
                changed = True
        if changed:
            slide_rels_tree.write(
                slide_rels_path, encoding="UTF-8", xml_declaration=True
            )

    # Repoint retained layouts to master 1. This also repairs Pandoc output
    # where the second-master relationship survived but the master part did not.
    for layout_path in layout_paths:
        layout_rels_path = layouts_dir / "_rels" / f"{layout_path.name}.rels"
        layout_rels_tree = ET.parse(layout_rels_path)
        changed = False
        for relationship in layout_rels_tree.getroot():
            if relationship.attrib.get("Type") == SLIDE_MASTER_REL:
                target = "../slideMasters/slideMaster1.xml"
                if relationship.attrib.get("Target") != target:
                    relationship.set("Target", target)
                    changed = True
        if changed:
            layout_rels_tree.write(
                layout_rels_path, encoding="UTF-8", xml_declaration=True
            )

    # Make master 1 the owner of every retained layout.
    master_rels_tree = ET.parse(master_rels_path)
    master_rels_root = master_rels_tree.getroot()
    removed_layout_rids: set[str] = set()
    for relationship in list(master_rels_root):
        target_name = Path(relationship.attrib.get("Target", "")).name
        if (
            relationship.attrib.get("Type") == SLIDE_LAYOUT_REL
            and target_name in removed_layout_names
        ):
            removed_layout_rids.add(relationship.attrib.get("Id", ""))
            master_rels_root.remove(relationship)
    existing_targets = {
        relationship.attrib.get("Target"): relationship.attrib.get("Id")
        for relationship in master_rels_root
        if relationship.attrib.get("Type") == SLIDE_LAYOUT_REL
    }
    numeric_rids = [
        int(match.group())
        for relationship in master_rels_root
        if (match := re.search(r"\d+", relationship.attrib.get("Id", "")))
    ]
    next_rid = max(numeric_rids, default=0) + 1
    layout_rids: dict[str, str] = {}
    for layout_path in layout_paths:
        target = f"../slideLayouts/{layout_path.name}"
        rid = existing_targets.get(target)
        if rid is None:
            rid = f"rId{next_rid}"
            next_rid += 1
            ET.SubElement(
                master_rels_root,
                f"{{{REL_NS}}}Relationship",
                {"Id": rid, "Target": target, "Type": SLIDE_LAYOUT_REL},
            )
        layout_rids[layout_path.name] = rid
    master_rels_tree.write(master_rels_path, encoding="UTF-8", xml_declaration=True)

    master_tree = ET.parse(master_path)
    master_root = master_tree.getroot()
    layout_id_list = master_root.find(f"{{{P_NS}}}sldLayoutIdLst")
    if layout_id_list is None:
        raise RuntimeError("primary slide master has no layout list")
    for node in list(layout_id_list):
        if node.attrib.get(f"{{{R_NS}}}id") in removed_layout_rids:
            layout_id_list.remove(node)
    listed_rids = {
        node.attrib.get(f"{{{R_NS}}}id") for node in list(layout_id_list)
    }
    numeric_layout_ids = [
        int(node.attrib["id"])
        for node in list(layout_id_list)
        if node.attrib.get("id", "").isdigit()
    ]
    next_layout_id = max(numeric_layout_ids, default=2147483647) + 1
    added_layouts = 0
    for layout_path in layout_paths:
        rid = layout_rids[layout_path.name]
        if rid in listed_rids:
            continue
        ET.SubElement(
            layout_id_list,
            f"{{{P_NS}}}sldLayoutId",
            {"id": str(next_layout_id), f"{{{R_NS}}}id": rid},
        )
        next_layout_id += 1
        added_layouts += 1
    master_tree.write(master_path, encoding="UTF-8", xml_declaration=True)

    for master_two_path in (
        masters_dir / "slideMaster2.xml",
        masters_dir / "_rels" / "slideMaster2.xml.rels",
    ):
        master_two_path.unlink(missing_ok=True)

    content_types = package_dir / "[Content_Types].xml"
    content_types_data = content_types.read_bytes()
    content_types_data = re.sub(
        rb'<Override\s+PartName="/ppt/slideMasters/slideMaster2\.xml"[^>]*/>',
        b"",
        content_types_data,
    )
    if removed_layout_names:
        removed_layout_pattern = b"|".join(
            re.escape(name.encode("ascii")) for name in sorted(removed_layout_names)
        )
        content_types_data = re.sub(
            rb'<Override\s+PartName="/ppt/slideLayouts/(?:'
            + removed_layout_pattern
            + rb')"[^>]*/>',
            b"",
            content_types_data,
        )
    content_types.write_bytes(content_types_data)
    return added_layouts


def remove_orphaned_presentation_relationships(package_dir: Path) -> list[str]:
    relationships = package_dir / "ppt" / "_rels" / "presentation.xml.rels"
    data = relationships.read_bytes()
    owner_dir = package_dir / "ppt"
    removed: list[str] = []

    def replace(match: re.Match[bytes]) -> bytes:
        tag = match.group(0)
        target_match = TARGET.search(tag)
        if target_match is None:
            return tag
        mode_match = TARGET_MODE.search(tag)
        if mode_match is not None and mode_match.group(1) == b"External":
            return tag
        target = target_match.group(1).decode("utf-8")
        if (owner_dir / target).resolve().exists():
            return tag
        removed.append(target)
        return b""

    repaired = RELATIONSHIP.sub(replace, data)
    if removed:
        relationships.write_bytes(repaired)
    return removed


def validate_relationship_targets(package_dir: Path) -> None:
    relationship_namespace = "{http://schemas.openxmlformats.org/package/2006/relationships}"
    missing: list[str] = []
    for relationships in package_dir.rglob("*.rels"):
        owner_dir = relationships.parent.parent
        for node in ET.parse(relationships).getroot():
            if node.tag != relationship_namespace + "Relationship":
                continue
            if node.attrib.get("TargetMode") == "External":
                continue
            target = node.attrib.get("Target")
            if target is None:
                continue
            if not (owner_dir / target).resolve().exists():
                missing.append(f"{relationships.relative_to(package_dir)} -> {target}")
    if missing:
        raise RuntimeError("missing relationship targets:\n" + "\n".join(missing))


def main() -> int:
    if len(sys.argv) != 2:
        print(f"usage: {Path(sys.argv[0]).name} UNPACKED_PPTX_DIR", file=sys.stderr)
        return 2

    package_dir = Path(sys.argv[1])
    slides_dir = package_dir / "ppt" / "slides"
    if not slides_dir.is_dir():
        print(f"missing slides directory: {slides_dir}", file=sys.stderr)
        return 2

    slide_paths = sorted(
        slides_dir.glob("slide*.xml"),
        key=lambda path: int(re.search(r"\d+", path.stem).group()),
    )
    changed_slides = 0
    changed_ids = 0
    for slide_path in slide_paths:
        changes = repair_slide(slide_path)
        if changes:
            changed_slides += 1
            changed_ids += changes

    added_master_layouts = collapse_to_single_master(package_dir)
    removed_relationships = remove_orphaned_presentation_relationships(package_dir)
    validate_relationship_targets(package_dir)

    print(
        f"Validated {len(slide_paths)} slides; repaired {changed_ids} duplicate "
        f"drawing IDs across {changed_slides} slides; "
        f"collapsed to one master and attached {added_master_layouts} layouts; "
        f"removed {len(removed_relationships)} orphaned metadata relationships"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
