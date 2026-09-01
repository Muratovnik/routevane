#!/usr/bin/env python3
"""Validate canonical Routevane skills and their Claude discovery adapters."""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
CANONICAL = ROOT / ".agents" / "skills"
ADAPTERS = ROOT / ".claude" / "skills"
NAME = re.compile(r"^[a-z0-9]+(?:-[a-z0-9]+)*$")
RETIRED_EXTERNAL_SKILLS = {"frontend-ui-engineering", "impeccable"}
RETIRED_PROJECT_PATHS = (".impeccable", ".playwright-mcp")


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
    skill_files = sorted(CANONICAL.glob("*/SKILL.md"))
    if not skill_files:
        failures.append("no canonical skills found")
    canonical_names = {path.parent.name for path in skill_files}
    adapter_names = {path.parent.name for path in ADAPTERS.glob("*/SKILL.md")}
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
        if not description or "TODO" in description:
            failures.append(f"{path.relative_to(ROOT)}: description is missing or unfinished")
        adapter = ADAPTERS / path.parent.name / "SKILL.md"
        if not adapter.is_file():
            failures.append(f"{adapter.relative_to(ROOT)}: discovery adapter is missing")
        elif adapter.read_text(encoding="utf-8") != expected_adapter(name, description):
            failures.append(
                f"{adapter.relative_to(ROOT)}: discovery adapter contract has drifted"
            )

    instruction_surfaces = [ROOT / "AGENTS.md", ROOT / ".gitignore"]
    instruction_surfaces.extend(skill_files)
    for path in instruction_surfaces:
        text = path.read_text(encoding="utf-8")
        for name in sorted(RETIRED_EXTERNAL_SKILLS):
            if re.search(rf"(?<![a-z0-9-]){re.escape(name)}(?![a-z0-9-])", text):
                failures.append(
                    f"{path.relative_to(ROOT)}: retired external skill reference {name!r}"
                )

    for relative in RETIRED_PROJECT_PATHS:
        if (ROOT / relative).exists():
            failures.append(f"{relative}: retired agent-tooling state must not return")

    if failures:
        for failure in failures:
            print(f"ERROR: {failure}", file=sys.stderr)
        return 1
    print(f"skills: {len(skill_files)} canonical definitions and adapters are valid")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
