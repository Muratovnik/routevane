#!/usr/bin/env python3
"""Check local documentation links and shared toolchain contracts, not prose quality."""

from __future__ import annotations

import json
import re
import subprocess
import sys
import tomllib
from pathlib import Path
from urllib.parse import unquote, urlsplit


ROOT = Path(__file__).resolve().parents[1]
LINK = re.compile(r"(?<!!)\[[^\]]*\]\(([^)]+)\)")
VERSION = re.compile(r"^>=([0-9]+)\.([0-9]+)\.([0-9]+) <([0-9]+)$")


def supported(version: str, requirement: str) -> bool:
    match = VERSION.fullmatch(requirement)
    actual = re.fullmatch(r"v?([0-9]+)\.([0-9]+)\.([0-9]+)", version)
    if not match or not actual:
        return False
    minimum = tuple(map(int, match.groups()[:3]))
    found = tuple(map(int, actual.groups()))
    return minimum <= found and found[0] < int(match.group(4))


def documentation_problems(root: Path, paths: list[str]) -> list[str]:
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
    paths = subprocess.check_output(
        ["git", "ls-files", "--cached", "--others", "--exclude-standard"], cwd=ROOT, text=True
    ).splitlines()
    failures = documentation_problems(ROOT, paths) + version_problems(ROOT) + scanner_problems(ROOT)
    for failure in failures:
        print(f"ERROR: {failure}", file=sys.stderr)
    if not failures:
        print("repository contracts: documentation paths and toolchain versions agree")
    return bool(failures)


if __name__ == "__main__":
    raise SystemExit(main())
