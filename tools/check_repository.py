#!/usr/bin/env python3
"""Check publication paths, documentation links and toolchain contracts, not prose quality."""

from __future__ import annotations

import json
import re
import subprocess
import sys
import tomllib
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
LINK = re.compile(r"\[[^\]]*\]\(([^)]+)\)")
VERSION = re.compile(r"^>=([0-9]+)\.([0-9]+)\.([0-9]+) <([0-9]+)$")
RETIRED_WORKING_DOCUMENTS = frozenset({
    "docs/history/implementation-plan.md",
    "docs/history/list-outputs-2026-08-22.md",
    "docs/history/ui-audit-2026-08-21.md",
    "docs/history/ui-redesign-2026-08-21.md",
    "docs/plans/routing-service-implementation-plan.md",
    "docs/plans/2026-08-22-list-outputs-brief.md",
    "docs/plans/2026-08-21-ui-redesign-brief.md",
    "docs/audits/2026-08-21-ui-critique.md",
})


def repository_paths(root: Path) -> list[str]:
    """Current source and publication candidates, not ignored workstation state."""
    paths = subprocess.check_output(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=root, encoding="utf-8",
    ).split("\0")
    return sorted({path for path in paths if path and (
        (root / path).is_file() or (root / path).is_symlink()
    )})


def supported(version: str, requirement: str) -> bool:
    match = VERSION.fullmatch(requirement)
    actual = re.fullmatch(r"v?([0-9]+)\.([0-9]+)\.([0-9]+)", version)
    if not match or not actual:
        return False
    minimum = tuple(map(int, match.groups()[:3]))
    found = tuple(map(int, actual.groups()))
    return minimum <= found and found[0] < int(match.group(4))


def publication_path_problems(root: Path, paths: list[str]) -> list[str]:
    failures = []
    for relative in sorted(set(paths)):
        source = root / relative
        if not (source.is_file() or source.is_symlink()):
            continue
        normalized = Path(relative).as_posix().casefold()
        if normalized in RETIRED_WORKING_DOCUMENTS:
            failures.append(
                f"{relative}: retired working-document path cannot be published; "
                "keep internal material in ignored .private/"
            )
    return failures


def documentation_status_problems(root: Path, paths: list[str]) -> list[str]:
    failures = []
    for relative in paths:
        path = root / relative
        if not relative.startswith("docs/") or path.suffix != ".md" or path.name == "README.md":
            continue
        header = re.match(r"\A---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)", path.read_text(encoding="utf-8"))
        if not header or not re.search(r"(?m)^status: (draft|adopted|superseded)\r?$", header[1]):
            failures.append(f"{relative}: missing valid status frontmatter")
    return failures


def documentation_problems(root: Path, paths: list[str]) -> list[str]:
    root = root.resolve()
    published_files = {
        root / path for path in paths
        if (root / path).is_file() or (root / path).is_symlink()
    }
    published_targets = published_files | {
        parent for path in published_files for parent in path.parents
        if parent.is_relative_to(root)
    }
    documents = {path for path in paths if path.endswith(".md") and (root / path).is_file()}
    edges: dict[str, set[str]] = {}
    failures: list[str] = []
    for relative in sorted(documents):
        source = root / relative
        text = source.read_text(encoding="utf-8")
        edges[relative] = set()
        for number, line in enumerate(text.splitlines(), 1):
            for match in LINK.finditer(line):
                target = match.group(1).strip().strip("<>")
                parsed = urlsplit(target)
                if parsed.scheme or not parsed.path:
                    continue
                resolved = (source.parent / unquote(parsed.path)).resolve()
                if not resolved.is_relative_to(root):
                    failures.append(f"{relative}:{number}: link escapes repository: {target}")
                elif not resolved.exists():
                    failures.append(f"{relative}:{number}: missing local link: {target}")
                elif resolved not in published_targets:
                    failures.append(f"{relative}:{number}: local link target is not publishable: {target}")
                else:
                    edges[relative].add(resolved.relative_to(root).as_posix())
    reachable: set[str] = set()
    pending = ["README.md", "CONTRIBUTING.md", "AGENTS.md", "SECURITY.md", "CHANGELOG.md"]
    while pending:
        item = pending.pop()
        if item not in reachable:
            reachable.add(item)
            pending.extend(edges.get(item, ()))
    for path in sorted(documents - reachable):
        if path.startswith(("docs/", "sdk/", "examples/")):
            failures.append(f"{path}: no documentation entry path reaches this file")
    return failures


def version_problems(root: Path) -> list[str]:
    package = json.loads((root / "web/package.json").read_text(encoding="utf-8"))
    lock = json.loads((root / "web/package-lock.json").read_text(encoding="utf-8"))
    engines = package["engines"]
    failures = []
    if engines != lock["packages"][""]["engines"]:
        failures.append("package-lock root engines differ from package.json")
    pin = (root / ".node-version").read_text(encoding="utf-8").strip()
    if not supported(pin, engines["node"]):
        failures.append(".node-version is outside the declared Node.js range")
    manager = package["packageManager"].removeprefix("npm@")
    if not supported(manager, engines["npm"]):
        failures.append("packageManager is outside the declared npm range")
    return failures


def scanner_problems(root: Path) -> list[str]:
    config = tomllib.loads((root / ".betterleaks.toml").read_text(encoding="utf-8"))
    return [
        f"secret scanner allowlist {index}: file and fixture criteria must both match (AND)"
        for index, allowance in enumerate(config.get("allowlists", []), 1)
        if allowance.get("paths") and allowance.get("regexes")
        and allowance.get("condition") != "AND"
    ]


def main() -> int:
    paths = repository_paths(ROOT)
    failures = (
        publication_path_problems(ROOT, paths) + documentation_problems(ROOT, paths)
        + documentation_status_problems(ROOT, paths)
        + version_problems(ROOT) + scanner_problems(ROOT)
    )
    for failure in failures:
        print(f"ERROR: {failure}", file=sys.stderr)
    if not failures:
        print("repository contracts: publication paths, documentation links and toolchain versions agree")
    return bool(failures)


if __name__ == "__main__":
    raise SystemExit(main())
