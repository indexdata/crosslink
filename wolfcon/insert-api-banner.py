#!/usr/bin/env python3
"""Add the API observability banner to its existing PowerPoint slide."""

from pathlib import Path
import re
import sys
import xml.etree.ElementTree as ET


P = "http://schemas.openxmlformats.org/presentationml/2006/main"
A = "http://schemas.openxmlformats.org/drawingml/2006/main"

ET.register_namespace("a", A)
ET.register_namespace("p", P)


def q(namespace: str, name: str) -> str:
    return f"{{{namespace}}}{name}"


def slide_number(path: Path) -> int:
    match = re.search(r"slide(\d+)\.xml$", path.name)
    return int(match.group(1)) if match else 0


def main() -> None:
    if len(sys.argv) != 3:
        raise SystemExit("usage: insert-api-banner.py PPTX_PACKAGE_DIR BANNER_TEXT")

    package = Path(sys.argv[1])
    banner_text = sys.argv[2]
    target_title = "One request, two complementary views"

    for slide_path in sorted(
        (package / "ppt/slides").glob("slide*.xml"), key=slide_number
    ):
        tree = ET.parse(slide_path)
        root = tree.getroot()
        shape_tree = root.find(f"./{q(P, 'cSld')}/{q(P, 'spTree')}")
        if shape_tree is None:
            continue
        texts = [node.text or "" for node in shape_tree.findall(f".//{q(A, 't')}")]
        if target_title not in texts:
            continue

        shape_ids = [
            int(node.get("id", "0"))
            for node in shape_tree.findall(f".//{q(P, 'cNvPr')}")
            if node.get("id", "0").isdigit()
        ]
        shape_id = max(shape_ids, default=1) + 1

        shape = ET.SubElement(shape_tree, q(P, "sp"))
        non_visual = ET.SubElement(shape, q(P, "nvSpPr"))
        ET.SubElement(
            non_visual,
            q(P, "cNvPr"),
            {"id": str(shape_id), "name": "API observability banner"},
        )
        ET.SubElement(non_visual, q(P, "cNvSpPr"))
        ET.SubElement(non_visual, q(P, "nvPr"))

        shape_properties = ET.SubElement(shape, q(P, "spPr"))
        transform = ET.SubElement(shape_properties, q(A, "xfrm"))
        ET.SubElement(transform, q(A, "off"), {"x": "731520", "y": "4846320"})
        ET.SubElement(transform, q(A, "ext"), {"cx": "10728960", "cy": "640080"})
        geometry = ET.SubElement(shape_properties, q(A, "prstGeom"), {"prst": "roundRect"})
        ET.SubElement(geometry, q(A, "avLst"))
        fill = ET.SubElement(shape_properties, q(A, "solidFill"))
        ET.SubElement(fill, q(A, "srgbClr"), {"val": "F7EDF4"})
        line = ET.SubElement(shape_properties, q(A, "ln"), {"w": "38100"})
        line_fill = ET.SubElement(line, q(A, "solidFill"))
        ET.SubElement(line_fill, q(A, "srgbClr"), {"val": "B31782"})

        text_body = ET.SubElement(shape, q(P, "txBody"))
        ET.SubElement(
            text_body,
            q(A, "bodyPr"),
            {
                "anchor": "ctr",
                "lIns": "137160",
                "rIns": "137160",
                "tIns": "45720",
                "bIns": "45720",
            },
        )
        ET.SubElement(text_body, q(A, "lstStyle"))
        paragraph = ET.SubElement(text_body, q(A, "p"))
        ET.SubElement(paragraph, q(A, "pPr"), {"algn": "ctr"})
        run = ET.SubElement(paragraph, q(A, "r"))
        run_properties = ET.SubElement(run, q(A, "rPr"), {"b": "1", "sz": "1400"})
        text_fill = ET.SubElement(run_properties, q(A, "solidFill"))
        ET.SubElement(text_fill, q(A, "srgbClr"), {"val": "002F56"})
        ET.SubElement(run, q(A, "t")).text = banner_text
        ET.SubElement(paragraph, q(A, "endParaRPr"), {"sz": "1400"})

        tree.write(slide_path, encoding="UTF-8", xml_declaration=True)
        return

    raise SystemExit(f"could not find slide titled: {target_title}")


if __name__ == "__main__":
    main()
