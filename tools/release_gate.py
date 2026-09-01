#!/usr/bin/env python3
"""Validate the immutable tag and changelog entry used for a release."""

from __future__ import annotations

import argparse
import re
import subprocess
from pathlib import Path
from typing import Callable, Sequence


ROOT = Path(__file__).resolve().parents[1]
STABLE_VERSION = re.compile(r"^v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$")
OBJECT_ID = re.compile(r"^[0-9a-fA-F]{40}(?:[0-9a-fA-F]{24})?$")
Git = Callable[[Sequence[str]], str]


def run_git(arguments: Sequence[str]) -> str:
    result = subprocess.run(
        ["git", *arguments],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return result.stdout.strip()


def changelog_entry(text: str, version: str) -> str:
    number = version.removeprefix("v")
    pattern = re.compile(
        rf"(?ms)^## \[{re.escape(number)}\][^\r\n]*(?:\r?\n|\Z).*?(?=^## |\Z)"
    )
    matches = list(pattern.finditer(text))
    if len(matches) != 1:
        raise ValueError(f"CHANGELOG.md must carry exactly one entry for {number}")
    entry = matches[0].group(0).strip()
    body = entry.splitlines()[1:]
    if not any(line.strip() for line in body):
        raise ValueError(f"CHANGELOG.md entry for {number} is empty")
    return entry + "\n"


def validate_release(version: str, expected_sha: str, changelog: str, git: Git = run_git) -> str:
    if not STABLE_VERSION.fullmatch(version):
        raise ValueError("release version must be a stable SemVer tag such as v0.1.0")
    if not OBJECT_ID.fullmatch(expected_sha):
        raise ValueError("release event SHA must be a full Git object ID")

    tag_ref = f"refs/tags/{version}"
    if git(("cat-file", "-t", tag_ref)) != "tag":
        raise ValueError(f"{version} must be an annotated tag")
    tagged_commit = git(("rev-parse", f"{tag_ref}^{{commit}}"))
    checkout_commit = git(("rev-parse", "HEAD"))
    expected = expected_sha.lower()
    if tagged_commit.lower() != expected:
        raise ValueError(f"{version} resolves to {tagged_commit}, not event SHA {expected_sha}")
    if checkout_commit.lower() != expected:
        raise ValueError(f"checked-out HEAD is {checkout_commit}, not event SHA {expected_sha}")
    return changelog_entry(changelog, version)


def self_test() -> None:
    sha = "1" * 40
    version = "v0.1.0"
    ref = f"refs/tags/{version}"
    values = {
        ("cat-file", "-t", ref): "tag",
        ("rev-parse", f"{ref}^{{commit}}"): sha,
        ("rev-parse", "HEAD"): sha,
    }

    def fake_git(arguments: Sequence[str]) -> str:
        return values[tuple(arguments)]

    notes = validate_release(
        version,
        sha,
        "# Changelog\n\n## [Unreleased]\n\n## [0.1.0] - 2026-09-01\n\n- First release.\n",
        fake_git,
    )
    assert notes.startswith("## [0.1.0]")
    for rejected in ("0.1.0", "v01.1.0", "v0.1.0-rc.1", "v';Get-Date;'x"):
        try:
            validate_release(rejected, sha, "", fake_git)
        except ValueError:
            pass
        else:
            raise AssertionError(f"unsafe version was accepted: {rejected}")

    values[("cat-file", "-t", ref)] = "commit"
    try:
        validate_release(version, sha, "## [0.1.0]\n\n- Entry.\n", fake_git)
    except ValueError as error:
        assert "annotated" in str(error)
    else:
        raise AssertionError("lightweight tag was accepted")
    values[("cat-file", "-t", ref)] = "tag"

    try:
        changelog_entry("## [0.1.0]\n\n## [0.1.0]\n", version)
    except ValueError:
        pass
    else:
        raise AssertionError("duplicate or empty changelog entries were accepted")
    print("release gate self-test: passed")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--version")
    parser.add_argument("--expected-sha")
    parser.add_argument("--changelog", type=Path, default=ROOT / "CHANGELOG.md")
    parser.add_argument("--notes", type=Path)
    args = parser.parse_args()

    if args.self_test:
        self_test()
        return 0
    if not args.version or not args.expected_sha or args.notes is None:
        parser.error("--version, --expected-sha, and --notes are required")
    notes = validate_release(
        args.version,
        args.expected_sha,
        args.changelog.read_text(encoding="utf-8"),
    )
    args.notes.parent.mkdir(parents=True, exist_ok=True)
    args.notes.write_text(notes, encoding="utf-8", newline="\n")
    print(f"release gate: {args.version} -> {args.expected_sha.lower()}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
