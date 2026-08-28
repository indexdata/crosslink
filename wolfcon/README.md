# WolfCON 2026 presentation

This directory contains the tracked source material for the WolfCON 2026
ReShare presentation.

- `wolfcon-2026-presentation.md` is the working narrative and content outline.
- `WolfCON-2026-ReShare-first-pass.md` is the renderable slide source.
- The SVG, CSS, and PowerPoint files are source assets and templates.
- Shared architecture diagrams remain in `misc/`.
- `ILL-wolfcon-2025.pptx` is the previous presentation used as reference.

Build the PowerPoint and PDF versions from the repository root:

```sh
./wolfcon/build-wolfcon-2026-deck.sh
./wolfcon/build-wolfcon-2026-pdf.sh
```

The generated `WolfCON-2026-ReShare-first-pass.pptx` and `.pdf` files are kept
in this directory for convenient review but are ignored by Git.
