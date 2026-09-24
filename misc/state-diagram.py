#!/usr/bin/env python3

"""
Generate state-transition matrix PDFs from StateModel YAML.

Rendering
=========

Action with transition to another state:
    Solid dark line + solid arrowhead.

Action without transition:
    Hollow dark circle only.

Action explicitly transitioning to the same state:
    Dark dot only.

Event with transition to another state:
    Dotted dark grey line + solid dark grey arrowhead.

Event without transition:
    Hollow dark grey circle only.

Event explicitly transitioning to the same state:
    Dark grey dot only.

Descriptions:
    Not rendered.

Labels:
    Actions use dark text, bold for primary actions; events use dark grey italics.
    Actions primary for any service type are shown in bold.
    Automatic actions have a lightning bolt before their names.
    A separate Type column shows icons: book (Loan), document (Copy),
    and a question-mark diamond (CopyOrLoan).

Service types:
    Include Loan and Copy by default. Use --service-types to select types.
    State, action/event, and target-state restrictions are intersected.

Action outcomes:
    "success" is implicit.

    Other outcomes are displayed inline:

        validate-patron [review]
        will-supply [failure]

    Each outcome gets its own table row because it can transition
    to a different target state.

Supported YAML structure
========================

Named models inside a stateModels mapping:

    stateModels:
      default:
        type: StateModel
        name: My State Model
        states:
          ...

      another:
        type: StateModel
        name: Another Model
        states:
          ...

Install
=======

    python3 -m venv .venv
    source .venv/bin/activate
    pip install -r requirements.txt

Usage
=====

    python state-diagram.py state-models.yaml -o state-model.pdf

    python state-diagram.py state-models.yaml \
        --model default \
        -o state-model.pdf

    python state-diagram.py state-models.yaml \
        --service-types Loan Copy CopyOrLoan \
        -o all-service-types.pdf
"""

import argparse

import yaml

from reportlab.pdfgen import canvas
from reportlab.lib.pagesizes import landscape, A2
from reportlab.lib import colors
from reportlab.lib.units import cm
from reportlab.pdfbase.pdfmetrics import stringWidth


PAGE_W, PAGE_H = landscape(A2)
SERVICE_TYPES = ("Loan", "Copy", "CopyOrLoan")
DEFAULT_SERVICE_TYPES = ("Loan", "Copy")


# ======================================================================
# YAML loading
# ======================================================================

def load_model(filename, model_key="default"):
    with open(filename, "r", encoding="utf-8") as f:
        data = yaml.safe_load(f)

    if not isinstance(data, dict):
        raise ValueError(
            "YAML root must be a mapping."
        )

    models = data.get("stateModels")
    if not isinstance(models, dict):
        raise ValueError(
            "YAML must contain a 'stateModels' mapping."
        )

    if model_key not in models:
        available = ", ".join(str(key) for key in models)
        raise ValueError(
            f"State model '{model_key}' not found. "
            f"Available models: {available}"
        )

    model = models[model_key]
    if not isinstance(model, dict):
        raise ValueError(
            f"State model '{model_key}' must be a mapping."
        )

    states = model.get("states")

    if not isinstance(states, list) or not states:
        raise ValueError(
            f"State model '{model_key}' does not contain "
            f"a non-empty 'states' list."
        )

    return model


# ======================================================================
# Model extraction
# ======================================================================

def sides_in_model(model):
    """
    Return sides in first-appearance order.
    """

    sides = []
    seen = set()

    for state in model["states"]:
        side = state.get("side")

        if not side:
            raise ValueError(
                f"State "
                f"'{state.get('name', '<unnamed>')}' "
                f"has no 'side'."
            )

        if side not in seen:
            seen.add(side)
            sides.append(side)

    return sides


def states_for_side(model, side):
    """
    Return states for one side while preserving YAML order.
    """

    return [
        state
        for state in model["states"]
        if state.get("side") == side
    ]


# ======================================================================
# Row extraction
# ======================================================================

def applicable_types(element, selected):
    """Intersect selected types with an element's optional restrictions."""
    allowed = element.get("appliesTo", {}).get("serviceTypes", SERVICE_TYPES)
    return tuple(kind for kind in selected if kind in allowed)


def build_rows(states, service_types=DEFAULT_SERVICE_TYPES):
    """
    Convert states into diagram rows.

    Preserve state order, listing each state's actions before its events.

    Each row is:

        {
            "kind": "action" | "event",
            "label": str,
            "source": str,
            "target": str | None,
            "service_types": tuple[str, ...],
            "primary": bool
        }

    Action rows additionally carry an "automatic" flag. Service types
    reflect the selected types and source, row, and destination restrictions.

    Examples:

        validate-patron
        validate-patron [review]
        will-supply [failure]
        cancel-request

    Descriptions are intentionally ignored.

    target=None means that the action/event is valid in the current
    state but does not cause a state transition.

    target == source means that YAML explicitly defines a transition
    back to the current state.

    An omitted transition is rendered as a hollow circle; an explicit
    self-transition is rendered as a filled dot.
    """

    rows = []

    for state in states:
        state_types = applicable_types(state, service_types)
        if not state_types:
            continue
        source = state.get("name")

        if not source:
            raise ValueError(
                "Every state must have a 'name'."
            )

        # ==========================================================
        # Actions
        # ==========================================================

        for action in state.get("actions", []) or []:
            if not isinstance(action, dict):
                raise ValueError(
                    f"Action in state '{source}' must be a mapping."
                )

            name = action.get("name")

            if not name:
                raise ValueError(
                    f"Action in state '{source}' has no 'name'."
                )

            action_types = applicable_types(action, state_types)
            if not action_types:
                continue
            transitions = action.get("transitions")
            primary_types = (
                action_types if name == state.get("primaryAction")
                else action.get("primaryFor", {}).get("serviceTypes", ())
            )

            # ------------------------------------------------------
            # Action without transition.
            #
            # Valid action; state remains unchanged.
            # ------------------------------------------------------

            if not transitions:
                rows.append(
                    {
                        "kind": "action",
                        "automatic": action.get("trigger") == "auto",
                        "primary_types": primary_types,
                        "service_types": action_types,
                        "label": str(name),
                        "source": source,
                        "target": None,
                    }
                )

                continue

            if not isinstance(transitions, dict):
                raise ValueError(
                    f"'transitions' for action '{name}' "
                    f"in state '{source}' must be a mapping."
                )

            # ------------------------------------------------------
            # One row per outcome.
            # ------------------------------------------------------

            for result, target in transitions.items():
                if not target:
                    raise ValueError(
                        f"Action '{name}' in state '{source}' "
                        f"has an empty target for transition "
                        f"'{result}'."
                    )

                label = str(name)

                # "success" is implicit.
                #
                # Other outcomes are displayed inline.
                if result != "success":
                    label += f" [{result}]"

                rows.append(
                    {
                        "kind": "action",
                        "automatic": action.get("trigger") == "auto",
                        "primary_types": primary_types,
                        "service_types": action_types,
                        "label": label,
                        "source": source,
                        "target": target,
                    }
                )

        # ==========================================================
        # Events
        # ==========================================================

        for event in state.get("events", []) or []:
            if not isinstance(event, dict):
                raise ValueError(
                    f"Event in state '{source}' must be a mapping."
                )

            name = event.get("name")

            if not name:
                raise ValueError(
                    f"Event in state '{source}' has no 'name'."
                )

            event_types = applicable_types(event, state_types)
            if not event_types:
                continue

            # transition is optional.
            #
            # No transition means the event is accepted while
            # remaining in the current state.
            target = event.get("transition")

            rows.append(
                {
                    "kind": "event",
                    "service_types": event_types,
                    "label": str(name),
                    "source": source,
                    "target": target,
                }
            )

    # A transition must also be applicable at its destination. Leave
    # unknown targets for validate_side to report rather than hiding them.
    states_by_name = {state["name"]: state for state in states}
    filtered_rows = []
    for row in rows:
        if row["target"] in states_by_name:
            row["service_types"] = applicable_types(
                states_by_name[row["target"]], row["service_types"]
            )
        if row["service_types"]:
            row["primary"] = bool(
                set(row.pop("primary_types", ())) & set(row["service_types"])
            )
            filtered_rows.append(row)
    return filtered_rows


# ======================================================================
# Validation
# ======================================================================

def validate_side(states, rows, side):
    """
    Validate states and transition references for one side.
    """

    names = [
        state.get("name")
        for state in states
    ]

    # --------------------------------------------------------------
    # Missing names
    # --------------------------------------------------------------

    for index, name in enumerate(names):
        if not name:
            raise ValueError(
                f"State #{index + 1} "
                f"on side '{side}' has no name."
            )

    # --------------------------------------------------------------
    # Duplicate names
    # --------------------------------------------------------------

    if len(names) != len(set(names)):
        duplicates = sorted(
            {
                name
                for name in names
                if names.count(name) > 1
            }
        )

        raise ValueError(
            f"Duplicate state name(s) "
            f"on side '{side}': "
            f"{', '.join(duplicates)}"
        )

    known_states = set(names)

    # --------------------------------------------------------------
    # Transition references
    # --------------------------------------------------------------

    for row in rows:
        kind = row["kind"]
        label = row["label"]
        source = row["source"]
        target = row["target"]

        if source not in known_states:
            raise ValueError(
                f"{kind.title()} '{label}' "
                f"references unknown source state "
                f"'{source}' on side '{side}'."
            )

        # target=None is valid.
        if (
            target is not None
            and target not in known_states
        ):
            raise ValueError(
                f"{kind.title()} '{label}' "
                f"in state '{source}' targets '{target}', "
                f"which is not defined on side '{side}'."
            )


# ======================================================================
# Row labels
# ======================================================================

def row_label_font(row):
    """Use the same label font for measurement and rendering."""
    if row["kind"] == "event":
        return "Helvetica-Oblique"
    return "Helvetica-Bold" if row.get("primary", False) else "Helvetica"


def draw_service_types(c, row, x, width, y, font_size, column_types):
    """Align each service type in a fixed slot within the Type column."""
    color = colors.HexColor("#4B5563" if row["kind"] == "event" else "#111827")
    c.setFillColor(color)
    c.setStrokeColor(color)
    slot_width = font_size * 1.3
    icons_width = len(column_types) * slot_width - font_size * 0.3
    left = x + (width - icons_width) / 2
    for index, service_type in enumerate(column_types):
        if service_type in row.get("service_types", ()):
            draw_service_icon(c, service_type, left + index * slot_width, y, font_size)


def draw_service_icon(c, service_type, x, y, size):
    """Draw a book, folded document, or question-mark diamond as vectors."""
    c.saveState()
    c.translate(x, y)
    c.scale(size, size)
    c.setLineWidth(0.07)
    c.setDash()
    path = c.beginPath()
    if service_type == "Loan":
        points = [(0.5, 0.05), (0.05, 0.2), (0.05, 0.95),
                  (0.5, 0.8), (0.95, 0.95), (0.95, 0.2)]
    elif service_type == "Copy":
        points = [(0.15, 0.05), (0.15, 0.95), (0.65, 0.95),
                  (0.85, 0.75), (0.85, 0.05)]
    else:
        points = [(0.5, 0), (1, 0.5), (0.5, 1), (0, 0.5)]
    path.moveTo(*points[0])
    for point in points[1:]:
        path.lineTo(*point)
    path.close()
    c.drawPath(path, stroke=1, fill=0)
    if service_type == "Loan":
        c.line(0.5, 0.05, 0.5, 0.8)
    elif service_type == "Copy":
        c.line(0.65, 0.95, 0.65, 0.75)
        c.line(0.65, 0.75, 0.85, 0.75)
        c.line(0.3, 0.5, 0.7, 0.5)
        c.line(0.3, 0.3, 0.7, 0.3)
    else:
        c.setFont("Helvetica-Bold", 0.65)
        c.drawCentredString(0.5, 0.27, "?")
    c.restoreState()


def draw_row_label(
    c,
    row,
    x,
    row_top,
    row_bottom,
    font_size,
):
    """
    Draw a single-line action/event label.

    Examples:

        validate-patron
        validate-patron [review]
        will-supply [failure]
        cancel-request
    """

    label = row["label"]
    kind = row["kind"]

    if kind == "action":
        color = colors.HexColor("#111827")
    else:
        color = colors.HexColor("#4B5563")

    c.setFillColor(color)

    c.setFont(
        row_label_font(row),
        font_size,
    )

    y_center = (
        row_top
        + row_bottom
    ) / 2

    # ReportLab positions text using its baseline.
    baseline_y = (
        y_center
        - font_size * 0.32
    )

    label_x = x + 8
    if row.get("automatic", False):
        # Draw a vector lightning bolt to avoid special font requirements.
        bolt_size = font_size * 0.8
        bolt_y = baseline_y + (font_size - bolt_size) / 2
        bolt = c.beginPath()
        points = [(0.65, 1), (0, 0.4), (0.3, 0.4),
                  (0.15, 0), (0.8, 0.6), (0.45, 0.6)]
        for index, (px, py) in enumerate(points):
            point = (label_x + px * bolt_size, bolt_y + py * bolt_size)
            if index == 0:
                bolt.moveTo(*point)
            else:
                bolt.lineTo(*point)
        bolt.close()
        c.drawPath(bolt, stroke=0, fill=1)
        label_x += font_size

    c.drawString(
        label_x,
        baseline_y,
        label,
    )


# ======================================================================
# Arrow rendering
# ======================================================================

def draw_arrow(
    c,
    x1,
    x2,
    y,
    kind,
):
    """
    Render a transition arrow.

    Actions:
        solid dark line
        solid dark arrowhead

    Events:
        dotted dark grey line
        solid dark grey arrowhead
    """

    # --------------------------------------------------------------
    # Style
    # --------------------------------------------------------------

    if kind == "action":
        color = colors.HexColor("#111827")

        c.setStrokeColor(color)
        c.setLineWidth(1.8)

        # Solid line.
        c.setDash()

    else:
        color = colors.HexColor("#4B5563")

        c.setStrokeColor(color)
        c.setLineWidth(1.8)

        # Dotted event line.
        c.setDash(
            1.5,
            2.5,
        )

    # --------------------------------------------------------------
    # Main transition line
    # --------------------------------------------------------------

    c.line(
        x1,
        y,
        x2,
        y,
    )

    # --------------------------------------------------------------
    # Arrowhead
    #
    # IMPORTANT:
    # Reset dash before drawing the arrowhead.
    #
    # This makes event lines dotted while keeping the arrowhead
    # solid and clearly visible.
    # --------------------------------------------------------------

    c.setDash()

    head = 7

    if x2 >= x1:
        c.line(
            x2,
            y,
            x2 - head,
            y + 3.5,
        )

        c.line(
            x2,
            y,
            x2 - head,
            y - 3.5,
        )

    else:
        c.line(
            x2,
            y,
            x2 + head,
            y + 3.5,
        )

        c.line(
            x2,
            y,
            x2 + head,
            y - 3.5,
        )


# ======================================================================
# State headers
# ======================================================================

def split_state_name(name):
    """
    Split state names at underscores.

    Example:

        CONDITION_PENDING

    becomes:

        CONDITION
        PENDING
    """

    return str(name).split("_")


# ======================================================================
# Page rendering
# ======================================================================

def draw_page(
    c,
    model,
    side,
    states,
    rows,
):
    margin_x = 1.2 * cm
    margin_y = 1.0 * cm

    # ==============================================================
    # Title
    # ==============================================================

    model_name = model.get(
        "name",
        "State Model",
    )

    title = (
        f"{model_name} - "
        f"{str(side).title()}"
    )

    title_y = (
        PAGE_H
        - margin_y
    )

    c.setFont(
        "Helvetica-Bold",
        22,
    )

    c.setFillColor(
        colors.HexColor("#0F172A")
    )

    c.drawString(
        margin_x,
        title_y,
        title,
    )

    # ==============================================================
    # Metadata
    #
    # Only the version is shown.
    # Descriptions are intentionally omitted.
    # ==============================================================

    top = (
        title_y
        - 40
    )

    if model.get("version") is not None:
        c.setFont(
            "Helvetica",
            8.5,
        )

        c.setFillColor(
            colors.HexColor("#64748B")
        )

        c.drawString(
            margin_x,
            title_y - 16,
            f"Version {model['version']}",
        )

    bottom = (
        margin_y
        + 0.4 * cm
    )

    # ==============================================================
    # Table geometry
    # ==============================================================

    header_h = (
        1.2 * cm
    )

    table_h = (
        top
        - bottom
    )

    body_h = (
        table_h
        - header_h
    )

    row_h = (
        body_h
        / max(1, len(rows))
    )

    label_font_size = min(
        8.5,
        max(
            5.5,
            row_h * 0.58,
        ),
    )

    # Match the fonts and left insets used when drawing labels and
    # the header, with equal padding on the right.
    row_label_w = max(
        max((
            stringWidth(
                row["label"],
                row_label_font(row),
                label_font_size,
            )
            + (label_font_size if row.get("automatic", False) else 0)
            for row in rows
        ), default=0) + 16,
        stringWidth("Action / ", "Helvetica-Bold", 11)
        + stringWidth("Event", "Helvetica-Oblique", 11)
        + 20,
    )

    column_types = tuple(
        kind for kind in SERVICE_TYPES
        if any(kind in row.get("service_types", ()) for row in rows)
    )
    service_column_w = max(
        stringWidth("Type", "Helvetica-Bold", 8),
        len(column_types) * label_font_size * 1.3,
    ) + 12

    state_area_w = PAGE_W - 2 * margin_x - row_label_w - service_column_w
    if state_area_w <= 0:
        raise ValueError(f"Action/event labels on side '{side}' exceed the page width.")
    state_w = state_area_w / len(states)

    x0 = margin_x
    y0 = bottom
    service_x = x0
    label_x = service_x + service_column_w

    x1 = (
        x0
        + row_label_w
        + service_column_w
    )

    y_top = (
        bottom
        + table_h
    )

    # ==============================================================
    # Outer border
    # ==============================================================

    c.setStrokeColor(
        colors.HexColor("#94A3B8")
    )

    c.setLineWidth(0.8)

    c.rect(
        x0,
        y0,
        (
            row_label_w
            + service_column_w
            + state_area_w
        ),
        table_h,
        stroke=1,
        fill=0,
    )

    # ==============================================================
    # Header backgrounds
    # ==============================================================

    c.setFillColor(
        colors.HexColor("#E2E8F0")
    )

    c.rect(
        x0,
        y_top - header_h,
        row_label_w + service_column_w,
        header_h,
        fill=1,
        stroke=0,
    )

    c.setFillColor(
        colors.HexColor("#F8FAFC")
    )

    c.rect(
        x1,
        y_top - header_h,
        state_area_w,
        header_h,
        fill=1,
        stroke=0,
    )

    # ==============================================================
    # Terminal state backgrounds
    # ==============================================================

    for idx, state in enumerate(states):
        if state.get("terminal") is True:
            c.setFillColor(
                colors.HexColor("#F1F5F9")
            )

            c.rect(
                (
                    x1
                    + idx * state_w
                ),
                y0,
                state_w,
                (
                    table_h
                    - header_h
                ),
                fill=1,
                stroke=0,
            )

    # ==============================================================
    # Grid
    # ==============================================================

    c.setStrokeColor(
        colors.HexColor("#CBD5E1")
    )

    c.setLineWidth(0.6)

    # Service-type/label separator; the state grid starts at x1.
    c.line(
        label_x,
        y0,
        label_x,
        y_top,
    )

    # State columns.
    for i in range(
        len(states) + 1
    ):
        xi = (
            x1
            + i * state_w
        )

        c.line(
            xi,
            y0,
            xi,
            y_top,
        )

    # Header separator.
    c.line(
        x0,
        y_top - header_h,
        (
            x0
            + row_label_w
            + service_column_w
            + state_area_w
        ),
        y_top - header_h,
    )

    # Transition rows.
    for i in range(
        len(rows) + 1
    ):
        yi = (
            y_top
            - header_h
            - i * row_h
        )

        c.line(
            x0,
            yi,
            (
                x0
                + row_label_w
                + service_column_w
                + state_area_w
            ),
            yi,
        )

    # ==============================================================
    # Left header
    # ==============================================================

    c.setFont(
        "Helvetica-Bold",
        11,
    )

    c.setFillColor(
        colors.HexColor("#0F172A")
    )

    c.drawString(
        label_x + 10,
        y_top - 0.78 * cm,
        "Action / ",
    )

    c.setFillColor(
        colors.HexColor("#4B5563")
    )

    c.setFont(
        "Helvetica-Oblique",
        11,
    )

    c.drawString(
        label_x + 10 + stringWidth("Action / ", "Helvetica-Bold", 11),
        y_top - 0.78 * cm,
        "Event",
    )

    # ==============================================================
    # Service-type and state headers
    # ==============================================================

    c.setFont("Helvetica-Bold", 8)
    c.setFillColor(colors.HexColor("#0F172A"))
    c.drawCentredString(
        service_x + service_column_w / 2,
        y_top - 0.78 * cm,
        "Type",
    )

    header_font = "Helvetica-Bold"
    preferred_font_size = 8.6
    minimum_font_size = 7.0
    # Keep padding proportional for very narrow columns.
    header_padding = min(2, state_w * 0.1)
    available_width = state_w - 2 * header_padding
    header_parts = [
        split_state_name(state["name"])
        for state in states
    ]
    widest_word = max(
        stringWidth(part, header_font, preferred_font_size)
        for parts in header_parts
        for part in parts
    )
    shared_font_size = max(
        minimum_font_size,
        min(
            preferred_font_size,
            preferred_font_size * available_width / max(widest_word, 1),
        ),
    )

    for idx, parts in enumerate(header_parts):

        col_x = (
            x1
            + idx * state_w
        )

        c.setFillColor(
            colors.HexColor("#0F172A")
        )

        y_header = y_top - (0.78 if len(parts) == 1 else 0.50) * cm

        for part in parts:
            # Keep words intact, shrinking only oversized words below
            # the shared minimum when necessary.
            word_width = stringWidth(part, header_font, shared_font_size)
            font_size = shared_font_size * min(
                1, available_width / max(word_width, 1)
            )
            c.setFont(header_font, font_size)
            text_w = stringWidth(
                part,
                header_font,
                font_size,
            )

            c.drawString(
                (
                    col_x
                    + (
                        state_w
                        - text_w
                    ) / 2
                ),
                y_header,
                part,
            )

            y_header -= 9

    # ==============================================================
    # State lookup
    # ==============================================================

    state_to_idx = {
        state["name"]: i
        for i, state
        in enumerate(states)
    }

    # ==============================================================
    # Render transition rows
    # ==============================================================

    for row_index, row in enumerate(rows):
        kind = row["kind"]
        source = row["source"]
        target = row["target"]

        row_top = (
            y_top
            - header_h
            - row_index * row_h
        )

        row_bottom = (
            row_top
            - row_h
        )

        y_center = (
            row_top
            + row_bottom
        ) / 2

        # ----------------------------------------------------------
        # Action/event label
        # ----------------------------------------------------------

        draw_row_label(
            c,
            row,
            label_x,
            row_top,
            row_bottom,
            label_font_size,
        )
        draw_service_types(
            c, row, service_x, service_column_w,
            y_center - label_font_size * 0.32, label_font_size, column_types,
        )

        # ----------------------------------------------------------
        # Source state position
        # ----------------------------------------------------------

        source_x = (
            x1
            + state_to_idx[source] * state_w
            + state_w / 2
        )

        # ----------------------------------------------------------
        # Transition color
        # ----------------------------------------------------------

        if kind == "action":
            dot_color = (
                colors.HexColor("#111827")
            )
        else:
            dot_color = (
                colors.HexColor("#4B5563")
            )

        # ----------------------------------------------------------
        # No effective state transition
        #
        # Two cases:
        #
        #   1. target is absent: hollow circle
        #   2. target explicitly equals source: filled dot
        # ----------------------------------------------------------

        if (
            target is None
            or target == source
        ):
            c.setFillColor(
                dot_color
            )
            c.setStrokeColor(dot_color)
            c.setLineWidth(1)
            c.setDash()

            c.circle(
                source_x,
                y_center,
                2.5,
                stroke=1 if target is None else 0,
                fill=0 if target is None else 1,
            )

            continue

        # ----------------------------------------------------------
        # Actual transition to another state
        # ----------------------------------------------------------

        target_x = (
            x1
            + state_to_idx[target] * state_w
            + state_w / 2
        )

        draw_arrow(
            c,
            source_x,
            target_x,
            y_center,
            kind,
        )

        # ----------------------------------------------------------
        # Source dot
        # ----------------------------------------------------------

        c.setFillColor(
            dot_color
        )

        c.circle(
            source_x,
            y_center,
            2.2,
            stroke=0,
            fill=1,
        )


# ======================================================================
# PDF generation
# ======================================================================

def generate_pdf(
    model,
    output_file,
    service_types=DEFAULT_SERVICE_TYPES,
):
    # Canonical order also removes duplicate CLI selections.
    if not service_types or set(service_types) - set(SERVICE_TYPES):
        raise ValueError("Select one or more of Loan, Copy, CopyOrLoan.")
    service_types = tuple(kind for kind in SERVICE_TYPES if kind in service_types)
    sides = sides_in_model(
        model
    )

    if not sides:
        raise ValueError(
            "No state sides found."
        )

    pages = []
    for side in sides:
        all_states = states_for_side(model, side)
        validate_side(all_states, build_rows(all_states, SERVICE_TYPES), side)
        states = [state for state in all_states if applicable_types(state, service_types)]
        rows = build_rows(all_states, service_types)
        if not states:
            continue
        validate_side(states, rows, side)
        pages.append((side, states, rows))

    if not pages:
        raise ValueError("No states apply to the selected service types.")

    c = canvas.Canvas(output_file, pagesize=landscape(A2))
    for page_number, (side, states, rows) in enumerate(pages):

        if page_number:
            c.showPage()

        draw_page(
            c,
            model,
            side,
            states,
            rows,
        )

    c.save()


# ======================================================================
# CLI
# ======================================================================

def main():
    parser = argparse.ArgumentParser(
        description=(
            "Generate a state-transition matrix PDF "
            "directly from StateModel YAML."
        )
    )

    parser.add_argument(
        "yaml_file",
        help="Input YAML file.",
    )

    parser.add_argument(
        "-o",
        "--output",
        default="state-model.pdf",
        help=(
            "Output PDF filename "
            "(default: state-model.pdf)."
        ),
    )

    parser.add_argument(
        "--service-types",
        nargs="+",
        choices=SERVICE_TYPES,
        default=DEFAULT_SERVICE_TYPES,
        help="Service types to include (default: Loan Copy).",
    )

    parser.add_argument(
        "--model",
        default="default",
        help=(
            "State model key inside the 'stateModels' mapping "
            "(default: default)."
        ),
    )

    args = parser.parse_args()

    model = load_model(
        args.yaml_file,
        args.model,
    )

    generate_pdf(
        model,
        args.output,
        args.service_types,
    )

    print(
        f"Generated {args.output}"
    )


if __name__ == "__main__":
    main()
