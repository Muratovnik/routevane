#!/usr/bin/env python3
"""Verify repaired dependency identities and monitor their original advisories."""

from __future__ import annotations

import json
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
BRACES_VERSION = "3.0.3-pn.3"
BRACES_INTEGRITY = "sha512-QY+Uq4s42STyIMPoRkBuUZfYyvz0uZuwuUburLwMx5N+lWqnHHaBxcKPtgKVKjTyFnS1q4ivKu9Wxi4VG7FE9Q=="
UPSTREAM_VERSIONS = {"braces": ["3.0.3"]}
# Recognition is tied to the reviewed repair and its runtime regressions, not a
# blanket exemption for the upstream package or severity.
REPAIRS = {
    "braces": {
        "url": "https://github.com/advisories/GHSA-vfj7-8cjw-p6xm",
        "title": "braces vulnerable to stack-exhaustion denial of service through deeply nested patterns",
        "severity": "high",
        "vulnerable_versions": "<=3.0.3",
    },
}


def load_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def verify_inventory(root: Path) -> None:
    package = load_json(root / "web/package.json")
    overrides = package["overrides"]
    if overrides.get("braces") != f"npm:@dieub/braces-depth-guard@{BRACES_VERSION}":
        raise ValueError("braces repair override does not match the reviewed identity")
    entries = load_json(root / "web/package-lock.json")["packages"]
    braces_entries = [entry for path, entry in entries.items()
                      if path.endswith("node_modules/braces")]
    if not braces_entries:
        raise ValueError("lockfile does not contain the repaired braces consumer")
    for entry in braces_entries:
        if (entry.get("name"), entry.get("version"), entry.get("integrity")) != (
            "@dieub/braces-depth-guard", BRACES_VERSION, BRACES_INTEGRITY
        ):
            raise ValueError("lockfile contains an unreviewed braces identity")


def advisory_problems(response: dict) -> list[str]:
    if not isinstance(response, dict) or set(response) - set(UPSTREAM_VERSIONS):
        raise ValueError("unexpected upstream advisory response")
    problems = []
    for name, advisories in response.items():
        if not isinstance(advisories, list):
            raise ValueError("invalid upstream advisory list")
        for advisory in advisories:
            if not isinstance(advisory, dict) or advisory.get("severity") not in (
                "info", "low", "moderate", "high", "critical"
            ):
                raise ValueError("invalid upstream advisory severity")
            repaired = REPAIRS[name]
            if all(advisory.get(key) == value for key, value in repaired.items()):
                print(f"verified repair: {name} {advisory['url']}")
                continue
            message = f"{name}: {advisory.get('severity')} {advisory.get('url', 'unknown advisory')}"
            if advisory["severity"] in ("high", "critical"):
                problems.append(message)
            else:
                print(message)
    return problems


def fetch_advisories() -> dict:
    request = urllib.request.Request(
        "https://registry.npmjs.org/-/npm/v1/security/advisories/bulk",
        data=json.dumps(UPSTREAM_VERSIONS).encode("utf-8"),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        data = response.read(2_000_001)
    if len(data) > 2_000_000:
        raise ValueError("upstream advisory response exceeds its limit")
    return json.loads(data)


def main() -> int:
    try:
        verify_inventory(ROOT)
        problems = advisory_problems(fetch_advisories())
        if problems:
            raise ValueError("new or changed upstream advisories: " + "; ".join(problems))
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"dependency repair check failed: {error}", file=sys.stderr)
        return 1
    print("dependency repairs: source identity and upstream advisory monitoring passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
