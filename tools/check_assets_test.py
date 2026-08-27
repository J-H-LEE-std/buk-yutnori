from __future__ import annotations

import json
import struct
import tempfile
import unittest
import zlib
from pathlib import Path

from tools.check_assets import AssetValidationError, validate_assets


def write_png(path: Path, width: int = 2, height: int = 2, color_type: int = 6) -> None:
    header = struct.pack(">IIBBBBB", width, height, 8, color_type, 0, 0, 0)
    chunk = lambda name, data: struct.pack(">I", len(data)) + name + data + struct.pack(">I", zlib.crc32(name + data) & 0xffffffff)
    path.write_bytes(b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", header) + chunk(b"IEND", b""))


class AssetCheckerTest(unittest.TestCase):
    def manifest(self, root: Path) -> Path:
        manifest = root.parent / "manifest.json"
        manifest.write_text(json.dumps([
            {"path": "board_main.png", "kind": "png", "width": 2, "height": 2, "critical": True},
            {"path": "font.ttf", "kind": "ttf"},
        ]), encoding="utf-8")
        return manifest

    def write_manifest(self, root: Path, entries: list[dict[str, object]]) -> Path:
        manifest = root.parent / "custom-manifest.json"
        manifest.write_text(json.dumps(entries), encoding="utf-8")
        return manifest

    def test_accepts_declared_rgba_png_and_ttf(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            write_png(root / "board_main.png")
            (root / "font.ttf").write_bytes(b"\x00\x01\x00\x00fixture")
            self.assertEqual(validate_assets(root, self.manifest(root)), 2)

    def test_rejects_missing_asset(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            with self.assertRaisesRegex(AssetValidationError, "required asset is missing"):
                validate_assets(root, self.manifest(root))

    def test_rejects_wrong_dimensions_and_extra_file(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            write_png(root / "board_main.png", width=4, height=2)
            (root / "font.ttf").write_bytes(b"\x00\x01\x00\x00fixture")
            (root / "not-declared.png").write_bytes(b"fixture")
            with self.assertRaises(AssetValidationError) as context:
                validate_assets(root, self.manifest(root))
            self.assertIn("expected 2x2", str(context.exception))
            self.assertIn("not declared", str(context.exception))

    def test_rejects_non_rgba_png(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            write_png(root / "board_main.png", color_type=2)
            (root / "font.ttf").write_bytes(b"\x00\x01\x00\x00fixture")
            with self.assertRaisesRegex(AssetValidationError, "must be RGBA8"):
                validate_assets(root, self.manifest(root))

    def test_rejects_non_snake_case_filename(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            write_png(root / "Board-Main.png")
            manifest = self.write_manifest(root, [{
                "path": "Board-Main.png", "kind": "png", "width": 2, "height": 2,
            }])
            with self.assertRaisesRegex(AssetValidationError, "snake_case"):
                validate_assets(root, manifest)

    def test_rejects_critical_budget_overrun(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            image = root / "board_main.png"
            write_png(image)
            with image.open("ab") as stream:
                stream.write(b"x" * (4 * 1024 * 1024))
            manifest = self.write_manifest(root, [{
                "path": "board_main.png", "kind": "png", "width": 2, "height": 2,
                "critical": True,
            }])
            with self.assertRaisesRegex(AssetValidationError, "critical assets exceed"):
                validate_assets(root, manifest)


if __name__ == "__main__":
    unittest.main()
