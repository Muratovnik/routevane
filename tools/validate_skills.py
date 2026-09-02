#!/usr/bin/env python3
"""Validate canonical Routevane skills and their Claude discovery adapters."""

from __future__ import annotations

import re
import sys
from pathlib import Path

from check_repository import repository_paths


ROOT = Path(__file__).resolve().parents[1]
CANONICAL = ROOT / ".agents" / "skills"
ADAPTERS = ROOT / ".claude" / "skills"
NAME = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")


def frontmatter(path: Path) -> dict[str, str]:
    lines = path.read_text(encoding="utf-8").splitlines()
    if not lines or lines[0] != "---":
        raise ValueError("missing opening frontmatter delimiter")
    try:
        end = lines.index("---", 1)
    except ValueError as error:
        raise ValueError("missing closing frontmatter delimiter") from error
    result: dict[str, str] = {}
    for line in lines[1:end]:
        key, separator, value = line.partition(":")
        if separator:
            result[key.strip()] = value.strip()
    return result


def expected_adapter(name: str, description: str) -> str:
    return (
        "---\n"
        f"name: {name}\n"
        f"description: {description}\n"
        "---\n\n"
        "# Claude discovery adapter\n\n"
        "This file provides Claude discovery only. Resolve the repository root that\n"
        f"contains it, read `.agents/skills/{name}/SKILL.md` completely, and\n"
        "follow that canonical skill. Do not continue from this adapter alone.\n"
    )


def main() -> int:
    failures: list[str] = []
    paths = {ROOT / relative for relative in repository_paths(ROOT)}
    skill_files = sorted(path for path in paths if path.parent.parent == CANONICAL and path.name == "SKILL.md")
    if not skill_files:
        failures.append("no canonical skills found")
    canonical_names = {path.parent.name for path in skill_files}
    adapter_names = {path.parent.name for path in paths if path.parent.parent == ADAPTERS and path.name == "SKILL.md"}
    for name in sorted(adapter_names - canonical_names):
        failures.append(f".claude/skills/{name}/SKILL.md: adapter has no canonical skill")

    for path in skill_files:
        try:
            metadata = frontmatter(path)
        except ValueError as error:
            failures.append(f"{path.relative_to(ROOT)}: {error}")
            continue
        name = metadata.get("name", "")
        description = metadata.get("description", "")
        if name != path.parent.name or not NAME.fullmatch(name):
            failures.append(f"{path.relative_to(ROOT)}: invalid or mismatched name")
        if not description:
            failures.append(f"{path.relative_to(ROOT)}: description is missing")
        adapter = ADAPTERS / path.parent.name / "SKILL.md"
        if adapter not in paths:
            failures.append(f"{adapter.relative_to(ROOT)}: discovery adapter is missing")
        elif adapter.read_text(encoding="utf-8") != expected_adapter(name, description):
            failures.append(
                f"{adapter.relative_to(ROOT)}: discovery adapter contract has drifted"
            )

    if failures:
        for failure in failures:
            print(f"ERROR: {failure}", file=sys.stderr)
        return 1
    print(f"skills: {len(skill_files)} canonical definitions and adapters are valid")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
