#!/usr/bin/env python3
"""Perform deterministic, network-free Markdown integrity checks."""

from __future__ import annotations

import re
from pathlib import Path
from urllib.parse import unquote


ROOT = Path(__file__).resolve().parents[1]
LINK_PATTERN = re.compile(r"(?<!!)\[[^\]]+\]\(([^)]+)\)")


def markdown_paths() -> list[Path]:
    return sorted(
        path
        for path in ROOT.rglob("*.md")
        if ".git" not in path.parts
    )


def validate() -> None:
    paths = markdown_paths()
    errors: list[str] = []

    for path in paths:
        text = path.read_text(encoding="utf-8")
        relative = path.relative_to(ROOT)
        first_content = next((line for line in text.splitlines() if line.strip()), "")
        if not first_content.startswith("# "):
            errors.append(f"{relative}: first content line must be one H1")
        if text and not text.endswith("\n"):
            errors.append(f"{relative}: missing final newline")

        for raw_target in LINK_PATTERN.findall(text):
            target = raw_target.strip()
            if (
                not target
                or target.startswith(("#", "http://", "https://", "mailto:"))
            ):
                continue
            target = target.split("#", 1)[0].strip("<>")
            resolved = (path.parent / unquote(target)).resolve()
            try:
                resolved.relative_to(ROOT.resolve())
            except ValueError:
                errors.append(f"{relative}: link escapes repository: {raw_target}")
                continue
            if not resolved.exists():
                errors.append(f"{relative}: broken local link: {raw_target}")

    if errors:
        raise AssertionError("\n".join(errors))
    print(f"DOCS_OK markdown={len(paths)}")


if __name__ == "__main__":
    validate()
