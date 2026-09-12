#!/usr/bin/env python3
"""Bind a stable annotated release tag to the checked-out event commit.

Curated notes are validated and exported by release-kit's notes command.
"""

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


def validate_release(version: str, expected_sha: str, git: Git = run_git, *, candidate: bool = False) -> None:
    if not STABLE_VERSION.fullmatch(version):
        raise ValueError("release version must be a stable SemVer tag such as v0.1.0")
    if not OBJECT_ID.fullmatch(expected_sha):
        raise ValueError("release event SHA must be a full Git object ID")

    if candidate:
        checkout_commit = git(("rev-parse", "HEAD"))
        if checkout_commit.lower() != expected_sha.lower():
            raise ValueError(f"checked-out HEAD is {checkout_commit}, not candidate SHA {expected_sha}")
        return
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

    validate_release(version, sha, fake_git)
    validate_release(version, sha, lambda args: sha if tuple(args) == ("rev-parse", "HEAD") else (_ for _ in ()).throw(AssertionError(args)), candidate=True)
    try:
        validate_release(version, sha, lambda _: "2" * 40, candidate=True)
    except ValueError:
        pass
    else:
        raise AssertionError("candidate with wrong source SHA was accepted")
    for rejected in ("0.1.0", "v01.1.0", "v0.1.0-rc.1", "v';Get-Date;'x"):
        try:
            validate_release(rejected, sha, fake_git)
        except ValueError:
            pass
        else:
            raise AssertionError(f"unsafe version was accepted: {rejected}")

    for rejected_sha in ("", "1" * 7, "g" * 40):
        try:
            validate_release(version, rejected_sha, fake_git)
        except ValueError:
            pass
        else:
            raise AssertionError(f"invalid event SHA was accepted: {rejected_sha}")

    for key, rejected, diagnostic in (
        (("cat-file", "-t", ref), "commit", "annotated"),
        (("rev-parse", f"{ref}^{{commit}}"), "2" * 40, "resolves to"),
        (("rev-parse", "HEAD"), "2" * 40, "checked-out HEAD"),
    ):
        previous = values[key]
        values[key] = rejected
        try:
            validate_release(version, sha, fake_git)
        except ValueError as error:
            assert diagnostic in str(error)
        else:
            raise AssertionError(f"invalid release identity was accepted: {key}")
        finally:
            values[key] = previous
    print("release gate self-test: passed")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--self-test", action="store_true")
    parser.add_argument("--candidate", action="store_true", help="Validate an explicit version and SHA before a tag exists")
    parser.add_argument("--version")
    parser.add_argument("--expected-sha")
    args = parser.parse_args()

    if args.self_test:
        self_test()
        return 0
    if not args.version or not args.expected_sha:
        parser.error("--version and --expected-sha are required")
    validate_release(args.version, args.expected_sha, candidate=args.candidate)
    print(f"release gate: {args.version} -> {args.expected_sha.lower()}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
