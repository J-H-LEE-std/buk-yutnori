#!/usr/bin/env python3
"""Validate branch, PR-title, tracking, and review-evidence policy."""

from __future__ import annotations

import argparse
import re


TYPES = "feat|fix|docs|test|refactor|perf|build|ci|chore|spike"
BRANCH_PATTERN = re.compile(
    rf"^(?P<type>{TYPES})/(?P<tracking>noissue|[1-9][0-9]*)-"
    r"[a-z0-9]+(?:-[a-z0-9]+)*$"
)
TITLE_PATTERN = re.compile(
    rf"^(?:{TYPES})(?:\([a-z0-9._/-]+\))?!?: .+"
)


def checked(body: str, label: str) -> bool:
    return re.search(
        rf"(?im)^-\s*\[[xX]\]\s*{re.escape(label)}\s*$",
        body,
    ) is not None


def field(body: str, label: str) -> str:
    match = re.search(rf"(?im)^{re.escape(label)}:\s*(.*?)\s*$", body)
    return match.group(1).strip() if match else ""


def require_evidence(body: str, heading: str) -> None:
    pattern = re.compile(
        rf"(?ims)^{re.escape(heading)}:\s*\n+(.*?)(?=\n## |\Z)"
    )
    match = pattern.search(body)
    value = match.group(1).strip() if match else ""
    value = re.sub(r"<!--.*?-->", "", value, flags=re.DOTALL).strip()
    if not value or value.lower() in {"n/a", "해당 없음", "없음"}:
        raise ValueError(f"{heading} is required")


def validate(branch: str, title: str, body: str, base: str = "main") -> None:
    if base != "main":
        raise ValueError("Pull Requests must target main")
    branch_match = BRANCH_PATTERN.fullmatch(branch)
    if not branch_match:
        raise ValueError("branch name does not match the repository policy")
    if not TITLE_PATTERN.fullmatch(title):
        raise ValueError("PR title does not follow Conventional Commits")

    milestone_text = field(body, "- 현재 Milestone")
    if not re.fullmatch(r"[0-6]", milestone_text):
        raise ValueError("PR body must declare current Milestone 0-6")
    milestone = int(milestone_text)

    tracking = branch_match.group("tracking")
    issue_text = field(body, "- Issue")
    trivial_docs = checked(
        body,
        "의미를 바꾸지 않는 단순 오탈자·링크·표현 수정",
    )
    canonical_change = checked(body, "정본 문서·spec·schema의 의미 변경")
    high_risk = checked(body, "고위험 변경")
    game_rule_change = checked(body, "게임 규칙의 의미 변경")
    if tracking == "noissue":
        if (
            branch_match.group("type") != "docs"
            or not trivial_docs
            or canonical_change
            or high_risk
            or game_rule_change
        ):
            raise ValueError("noissue is only valid for explicitly classified trivial docs")
        if issue_text.lower() != "noissue":
            raise ValueError("noissue branch must declare Issue: noissue")
    elif not re.search(rf"#\s*{re.escape(tracking)}\b", issue_text):
        raise ValueError("PR body must reference the branch Issue number")

    if not checked(body, "자기 검토 완료"):
        raise ValueError("self-review checklist is not complete")
    if not checked(body, "Codex 자체 리뷰 완료"):
        raise ValueError("Codex review checklist is not complete")
    require_evidence(body, "Codex 리뷰 증빙")

    if milestone >= 2 and high_risk:
        require_evidence(body, "독립 리뷰 증빙")
    if game_rule_change:
        require_evidence(body, "사용자 승인 증빙")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--branch", required=True)
    parser.add_argument("--base", required=True)
    parser.add_argument("--title", required=True)
    parser.add_argument("--body", default="")
    args = parser.parse_args()
    validate(args.branch, args.title, args.body, args.base)
    print("PR_POLICY_OK")


if __name__ == "__main__":
    main()
