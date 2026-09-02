#!/usr/bin/env python3
"""Validate Routevane commit subjects against the repository contract."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


SUBJECT = re.compile(
    r"^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)"
    r"(?:\([a-z0-9][a-z0-9-]*(?:/[a-z0-9][a-z0-9-]*)*\))?!?: "
    r"\S(?:[^\r\n]*\S)?$"
)


def validate(subject: str) -> list[str]:
    problems: list[str] = []
    if not SUBJECT.fullmatch(subject):
        problems.append(
            "subject must use the closed Angular type set, an optional lowercase "
            "scope, and a non-empty single-line description"
        )
    return problems


def subjects_from_history() -> list[str]:
    result = subprocess.run(
        ["git", "log", "--no-merges", "--format=%s"],
        check=True,
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    return [line for line in result.stdout.splitlines() if line]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("message_file", nargs="?", type=Path)
    parser.add_argument("--history", action="store_true")
    args = parser.parse_args()

    if args.history:
        subjects = subjects_from_history()
    elif args.message_file:
        lines = args.message_file.read_text(encoding="utf-8").splitlines()
        subjects = [lines[0]] if lines else [""]
    else:
        parser.error("provide a commit message file or --history")

    failed = False
    for subject in subjects:
        problems = validate(subject)
        if problems:
            failed = True
            print(f"invalid commit subject: {subject!r}", file=sys.stderr)
            for problem in problems:
                print(f"  - {problem}", file=sys.stderr)
    return 1 if failed else 0


if __name__ == "__main__":
    raise SystemExit(main())
