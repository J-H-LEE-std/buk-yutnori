#!/usr/bin/env python3
"""Validate immutable annotated SemVer release tags against remote main."""

from __future__ import annotations

import argparse
import re
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
SEMVER_TAG = re.compile(r"v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)")


class ReleaseTagPolicyError(RuntimeError):
    """Report a release tag that does not satisfy repository policy."""


def git_output(repository: Path, *args: str) -> str:
    return subprocess.check_output(
        ["git", *args],
        cwd=repository,
        text=True,
        stderr=subprocess.STDOUT,
    ).strip()


def validate_release_tag(
    tag: str,
    repository: Path = ROOT,
    remote: str = "origin",
    main_branch: str = "main",
) -> str:
    """Return the target commit when tag is annotated, SemVer, and on main."""

    if not SEMVER_TAG.fullmatch(tag):
        raise ReleaseTagPolicyError("Tag must be strict SemVer vMAJOR.MINOR.PATCH")

    tag_ref = f"refs/tags/{tag}"
    main_ref = f"refs/remotes/{remote}/{main_branch}"
    fetch = subprocess.run(
        [
            "git",
            "fetch",
            "--force",
            "--no-tags",
            remote,
            f"{tag_ref}:{tag_ref}",
            f"refs/heads/{main_branch}:{main_ref}",
        ],
        cwd=repository,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
    )
    if fetch.returncode != 0:
        details = fetch.stdout.strip()
        raise ReleaseTagPolicyError(
            f"Unable to fetch release tag and main from {remote}: {details}"
        )

    tag_type = git_output(repository, "cat-file", "-t", tag_ref)
    if tag_type != "tag":
        raise ReleaseTagPolicyError("Release tags must be annotated tags")

    target = git_output(repository, "rev-parse", f"{tag_ref}^{{}}")
    on_main = subprocess.run(
        ["git", "merge-base", "--is-ancestor", target, main_ref],
        cwd=repository,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    )
    if on_main.returncode != 0:
        raise ReleaseTagPolicyError("Release tag target must be reachable from main")
    return target


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tag", required=True)
    args = parser.parse_args()

    try:
        target = validate_release_tag(args.tag)
    except (ReleaseTagPolicyError, subprocess.CalledProcessError) as error:
        raise SystemExit(str(error)) from error
    print(f"RELEASE_TAG_POLICY_OK tag={args.tag} target={target}")


if __name__ == "__main__":
    main()
