#!/usr/bin/env python3
"""Validate the GUI asset contract from docs/15_gui_assets.md.

The checker is intentionally dependency-free.  It validates a supplied asset
root (or ``client/assets`` by default) and fails closed on missing, malformed,
unexpectedly named, or oversized resources.
"""

from __future__ import annotations

import argparse
import json
import re
import struct
from dataclasses import dataclass
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"
NAME_PATTERN = re.compile(r"^[a-z0-9]+(?:_[a-z0-9]+)*\.(?:png|ttf)$")


@dataclass(frozen=True)
class AssetRule:
    path: str
    kind: str
    width: int | None = None
    height: int | None = None
    critical: bool = False


DEFAULT_RULES = (
    AssetRule("board/board_main.png", "png", 1024, 1024, True),
    AssetRule("board/node_marker.png", "png", 48, 48, True),
    AssetRule("board/node_marker_last.png", "png", 48, 48, True),
    AssetRule("board/path_highlight.png", "png", 48, 48, True),
    *(AssetRule(f"piece/{team}_{state}.png", "png", 96, 96, True)
      for team in ("a", "b")
      for state in ("on_board", "home_checkpoint", "finished", "waiting")),
    AssetRule("piece/movable_outline.png", "png", 112, 112, True),
    AssetRule("piece/finished_crown.png", "png", 64, 64, True),
    *(AssetRule(f"yut/result_{result}.png", "png", 256, 256, True)
      for result in ("do", "gae", "geol", "yut", "mo", "backdo", "buk")),
    *(AssetRule(f"yut/toss_{index:02d}.png", "png", 256, 256, True)
      for index in range(8)),
    AssetRule("fx/capture_flash.png", "png", 256, 256, True),
    AssetRule("fx/stack_pop.png", "png", 256, 256, True),
    AssetRule("gui/common/panel.png", "png"),
    AssetRule("gui/common/button_normal.png", "png"),
    AssetRule("gui/common/button_hover.png", "png"),
    AssetRule("gui/common/button_pressed.png", "png"),
    AssetRule("gui/common/button_disabled.png", "png"),
    AssetRule("gui/common/slot_frame.png", "png"),
    AssetRule("gui/common/badge_ready.png", "png"),
    AssetRule("gui/common/badge_watch.png", "png"),
    AssetRule("gui/common/marker_team_a.png", "png"),
    AssetRule("gui/common/marker_team_b.png", "png"),
    AssetRule("gui/common/stack_count.png", "png"),
    AssetRule("gui/common/menu_frame.png", "png"),
    AssetRule("gui/common/modal_frame.png", "png"),
    AssetRule("screen/game/hud_frame.png", "png"),
    AssetRule("screen/game/result_queue_panel.png", "png"),
    AssetRule("screen/game/turn_banner.png", "png"),
    AssetRule("font/notosans_kr_regular.ttf", "ttf"),
    AssetRule("font/notosans_kr_bold.ttf", "ttf"),
)


class AssetValidationError(ValueError):
    """Raised when an asset tree violates the documented contract."""


def _rules_from_manifest(path: Path | None) -> tuple[AssetRule, ...]:
    if path is None:
        return tuple(DEFAULT_RULES)
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise AssetValidationError("manifest must be a JSON array")
    rules = []
    for item in data:
        if not isinstance(item, dict) or not isinstance(item.get("path"), str):
            raise AssetValidationError("manifest entries require a path")
        rules.append(AssetRule(item["path"], item.get("kind", "png"),
                               item.get("width"), item.get("height"),
                               bool(item.get("critical", False))))
    return tuple(rules)


def _png_size_and_format(path: Path) -> tuple[int, int]:
    raw = path.read_bytes()
    if len(raw) < 33 or raw[:8] != PNG_SIGNATURE:
        raise AssetValidationError(f"{path}: invalid PNG signature")
    length, chunk = struct.unpack(">I4s", raw[8:16])
    if chunk != b"IHDR" or length != 13 or len(raw) < 29:
        raise AssetValidationError(f"{path}: missing PNG IHDR")
    width, height, depth, color_type = struct.unpack(">IIBB", raw[16:26])
    if depth != 8 or color_type != 6:
        raise AssetValidationError(f"{path}: PNG must be RGBA8")
    return width, height


def validate_assets(root: Path, manifest: Path | None = None,
                    initial_budget: int = 4 * 1024 * 1024,
                    total_budget: int = 16 * 1024 * 1024) -> int:
    rules = _rules_from_manifest(manifest)
    errors: list[str] = []
    expected = {rule.path for rule in rules}
    total = 0
    initial = 0
    for rule in rules:
        relative = Path(rule.path)
        if relative.is_absolute() or ".." in relative.parts:
            errors.append(f"{rule.path}: path must stay below asset root")
            continue
        path = root / relative
        if not path.is_file():
            errors.append(f"{rule.path}: required asset is missing")
            continue
        if not NAME_PATTERN.fullmatch(path.name):
            errors.append(f"{rule.path}: filename must be snake_case")
        size = path.stat().st_size
        total += size
        if rule.critical:
            initial += size
        try:
            if rule.kind == "png":
                width, height = _png_size_and_format(path)
                if rule.width is not None and (width, height) != (rule.width, rule.height):
                    errors.append(f"{rule.path}: expected {rule.width}x{rule.height}, got {width}x{height}")
            elif rule.kind == "ttf":
                if path.read_bytes()[:4] not in (b"\x00\x01\x00\x00", b"OTTO"):
                    errors.append(f"{rule.path}: invalid TTF signature")
            else:
                errors.append(f"{rule.path}: unsupported asset kind {rule.kind!r}")
        except (OSError, struct.error) as exc:
            errors.append(f"{rule.path}: cannot inspect asset ({exc})")
    for path in root.rglob("*") if root.exists() else ():
        if path.is_file() and path.relative_to(root).as_posix() not in expected:
            errors.append(f"{path.relative_to(root)}: not declared in manifest")
    if initial > initial_budget:
        errors.append(f"critical assets exceed {initial_budget} bytes ({initial})")
    if total > total_budget:
        errors.append(f"all assets exceed {total_budget} bytes ({total})")
    if errors:
        raise AssetValidationError("\n".join(errors))
    return len(rules)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT / "client/assets")
    parser.add_argument("--manifest", type=Path)
    args = parser.parse_args()
    count = validate_assets(args.root, args.manifest)
    print(f"ASSETS_OK files={count} root={args.root}")


if __name__ == "__main__":
    main()
