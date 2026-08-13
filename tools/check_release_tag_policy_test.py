from __future__ import annotations

import subprocess
import tempfile
import unittest
from collections.abc import Iterator
from contextlib import contextmanager
from pathlib import Path

from tools.check_release_tag_policy import ReleaseTagPolicyError, validate_release_tag


class ReleaseTagPolicyTest(unittest.TestCase):
    def test_restores_annotated_tag_after_checkout_flattens_local_ref(self) -> None:
        with self.repository() as repository:
            self.git(repository, "tag", "-a", "v0.1.0", "-m", "Milestone 1")
            self.git(repository, "push", "origin", "v0.1.0")
            target = self.git(repository, "rev-parse", "HEAD")

            self.git(repository, "update-ref", "refs/tags/v0.1.0", target)
            self.assertEqual(self.git(repository, "cat-file", "-t", "v0.1.0"), "commit")

            self.assertEqual(validate_release_tag("v0.1.0", repository), target)
            self.assertEqual(self.git(repository, "cat-file", "-t", "v0.1.0"), "tag")

    def test_rejects_lightweight_tag(self) -> None:
        with self.repository() as repository:
            self.git(repository, "tag", "v0.1.0")
            self.git(repository, "push", "origin", "v0.1.0")

            with self.assertRaisesRegex(ReleaseTagPolicyError, "annotated"):
                validate_release_tag("v0.1.0", repository)

    def test_rejects_tag_target_outside_main(self) -> None:
        with self.repository() as repository:
            tree = self.git(repository, "rev-parse", "HEAD^{tree}")
            target = self.git(repository, "commit-tree", tree, "-m", "release-only")
            self.git(repository, "tag", "-a", "v0.1.0", target, "-m", "Milestone 1")
            self.git(repository, "push", "origin", "v0.1.0")

            with self.assertRaisesRegex(ReleaseTagPolicyError, "reachable from main"):
                validate_release_tag("v0.1.0", repository)

    def test_rejects_non_semver_tag_before_fetch(self) -> None:
        with self.repository() as repository:
            with self.assertRaisesRegex(ReleaseTagPolicyError, "strict SemVer"):
                validate_release_tag("release-1", repository)

    @staticmethod
    def git(repository: Path, *args: str) -> str:
        return subprocess.check_output(
            ["git", *args],
            cwd=repository,
            text=True,
            stderr=subprocess.DEVNULL,
        ).strip()

    @contextmanager
    def repository(self) -> Iterator[Path]:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            remote = root / "remote.git"
            repository = root / "repository"
            subprocess.run(
                ["git", "init", "--bare", str(remote)],
                check=True,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            subprocess.run(
                ["git", "init", "-b", "main", str(repository)],
                check=True,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
            )
            self.git(repository, "config", "user.name", "Release Policy Test")
            self.git(repository, "config", "user.email", "release-policy@example.invalid")
            (repository / "README.md").write_text("release policy fixture\n", encoding="utf-8")
            self.git(repository, "add", "README.md")
            self.git(repository, "commit", "-m", "test: initialize fixture")
            self.git(repository, "remote", "add", "origin", str(remote))
            self.git(repository, "push", "-u", "origin", "main")
            yield repository


if __name__ == "__main__":
    unittest.main()
