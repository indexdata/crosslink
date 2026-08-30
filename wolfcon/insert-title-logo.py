#!/usr/bin/env python3
"""Insert the WolfCON logo into the generated presentation title slide."""

from pathlib import Path
import sys
import xml.etree.ElementTree as ET


P = "http://schemas.openxmlformats.org/presentationml/2006/main"
A = "http://schemas.openxmlformats.org/drawingml/2006/main"
R = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
PR = "http://schemas.openxmlformats.org/package/2006/relationships"

ET.register_namespace("a", A)
ET.register_namespace("p", P)
ET.register_namespace("r", R)


def q(namespace: str, name: str) -> str:
    return f"{{{namespace}}}{name}"


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: insert-title-logo.py PPTX_PACKAGE_DIR")

    package = Path(sys.argv[1])
    slide_path = package / "ppt/slides/slide1.xml"
    rels_path = package / "ppt/slides/_rels/slide1.xml.rels"
    logo_path = package / "ppt/media/wolfcon-2026-logo.png"

    if not logo_path.is_file():
        raise SystemExit(f"missing title logo: {logo_path}")

    slide_tree = ET.parse(slide_path)
    slide_root = slide_tree.getroot()
    shape_tree = slide_root.find(f"./{q(P, 'cSld')}/{q(P, 'spTree')}")
    if shape_tree is None:
        raise SystemExit("title slide has no shape tree")

    for node in shape_tree.findall(f".//{q(P, 'cNvPr')}"):
        if node.get("name") == "WolfCON 2026 logo":
            return

    # Pandoc retains the template's white title text but not the original dark
    # slide-level background. Make the generated title readable on white.
    for shape in shape_tree.findall(q(P, "sp")):
        placeholder = shape.find(f"./{q(P, 'nvSpPr')}/{q(P, 'nvPr')}/{q(P, 'ph')}")
        if placeholder is None or placeholder.get("type") != "ctrTitle":
            continue
        for run_properties in shape.findall(f".//{q(A, 'rPr')}"):
            for fill in run_properties.findall(q(A, "solidFill")):
                run_properties.remove(fill)
            solid_fill = ET.SubElement(run_properties, q(A, "solidFill"))
            ET.SubElement(solid_fill, q(A, "srgbClr"), {"val": "053754"})

    rels_tree = ET.parse(rels_path)
    rels_root = rels_tree.getroot()
    used_rel_ids = {rel.get("Id", "") for rel in rels_root}
    rel_number = 1
    while f"rId{rel_number}" in used_rel_ids:
        rel_number += 1
    rel_id = f"rId{rel_number}"

    ET.SubElement(
        rels_root,
        q(PR, "Relationship"),
        {
            "Id": rel_id,
            "Type": f"{R}/image",
            "Target": "../media/wolfcon-2026-logo.png",
        },
    )

    shape_ids = [
        int(node.get("id", "0"))
        for node in shape_tree.findall(f".//{q(P, 'cNvPr')}")
        if node.get("id", "0").isdigit()
    ]
    shape_id = max(shape_ids, default=1) + 1

    picture = ET.SubElement(shape_tree, q(P, "pic"))
    non_visual = ET.SubElement(picture, q(P, "nvPicPr"))
    ET.SubElement(
        non_visual,
        q(P, "cNvPr"),
        {"id": str(shape_id), "name": "WolfCON 2026 logo"},
    )
    picture_properties = ET.SubElement(non_visual, q(P, "cNvPicPr"))
    ET.SubElement(picture_properties, q(A, "picLocks"), {"noChangeAspect": "1"})
    ET.SubElement(non_visual, q(P, "nvPr"))

    fill = ET.SubElement(picture, q(P, "blipFill"))
    ET.SubElement(fill, q(A, "blip"), {q(R, "embed"): rel_id})
    ET.SubElement(fill, q(A, "srcRect"))
    ET.SubElement(fill, q(A, "stretch"))

    shape_properties = ET.SubElement(picture, q(P, "spPr"))
    transform = ET.SubElement(shape_properties, q(A, "xfrm"))
    ET.SubElement(transform, q(A, "off"), {"x": "320040", "y": "228600"})
    ET.SubElement(transform, q(A, "ext"), {"cx": "1600200", "cy": "1653000"})
    geometry = ET.SubElement(shape_properties, q(A, "prstGeom"), {"prst": "rect"})
    ET.SubElement(geometry, q(A, "avLst"))

    slide_tree.write(slide_path, encoding="UTF-8", xml_declaration=True)
    rels_tree.write(rels_path, encoding="UTF-8", xml_declaration=True)


if __name__ == "__main__":
    main()
