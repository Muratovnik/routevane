#!/usr/bin/env python3
"""Repository diagnostics, using the existing Go-pinned YAML parser for workflows."""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import sys
from pathlib import Path

from check_repository import repository_paths, supported


ROOT = Path(__file__).resolve().parents[1]
REQUIRED = (
    "AGENTS.md",
    "README.md",
    "go.mod",
    "go.sum",
    "web/package.json",
    "web/package-lock.json",
    ".githooks/pre-commit",
    ".githooks/commit-msg",
    ".github/workflows/ci.yml",
    ".github/workflows/release.yml",
    "tools/release_gate.py",
    "tools/release_metadata.py",
    "tools/release_archive.py",
    "tools/release_smoke.py",
    "tools/check_repository.py",
    "tools/workflowcheck/main.go",
    "dev.cmd",
)


def command_version(command: list[str], timeout: float = 10) -> str:
    result = subprocess.run(
        command,
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        timeout=timeout,
    )
    lines = (result.stdout or result.stderr).strip().splitlines()
    if not lines:
        raise ValueError(f"{command[0]} did not report a version")
    return lines[0]


def required_go_version(root: Path) -> tuple[int, int, int]:
    match = re.search(
        r"(?m)^go[ \t]+(\d+)\.(\d+)\.(\d+)[ \t]*(?://[^\n]*)?$",
        (root / "go.mod").read_text(encoding="utf-8"),
    )
    if match is None:
        raise ValueError("go.mod must declare the supported Go minor and minimum patch")
    return tuple(map(int, match.groups()))


def supported_go(version: str, minimum: tuple[int, int, int]) -> bool:
    match = re.fullmatch(r"go version go(\d+)\.(\d+)\.(\d+) \S+", version)
    if match is None:
        return False
    actual = tuple(map(int, match.groups()))
    return actual[:2] == minimum[:2] and actual >= minimum


def workflow_problems(root: Path, go: str) -> list[str]:
    try:
        workflows = [path for path in repository_paths(root)
                     if Path(path).parent == Path(".github/workflows")
                     and Path(path).suffix in (".yml", ".yaml")]
    except (OSError, subprocess.CalledProcessError) as error:
        return [f"workflow inventory unavailable: {error}"]
    failures = [f"{path}: required workflow is not publishable"
                for path in (".github/workflows/ci.yml", ".github/workflows/release.yml")
                if path not in workflows]
    if not workflows:
        return failures
    try:
        result = subprocess.run(
            [go, "run", "-mod=readonly", "./tools/workflowcheck", *workflows],
            cwd=root, capture_output=True, text=True, encoding="utf-8", errors="replace",
            timeout=180,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        return failures + [f"workflow YAML validation could not run: {error}"]
    if result.returncode:
        return failures + [f"workflow YAML validation failed: {(result.stderr or result.stdout).strip()}"]
    for relative in workflows:
        text = (root / relative).read_text(encoding="utf-8")
        if "audit --history --owner" in text:
            failures.append(
                f"{relative}: owner policy requires a private maintainer checkout "
                "and must not run in public CI"
            )
        if relative in (".github/workflows/ci.yml", ".github/workflows/release.yml"):
            if "python .github/relkit.pyz audit --history" not in text:
                failures.append(f"{relative}: public history gate is missing")
    return failures


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--go", help="Go executable selected by the developer entrypoint")
    options = parser.parse_args([] if argv is None else argv)
    failures: list[str] = []
    for relative in REQUIRED:
        if not (ROOT / relative).is_file():
            failures.append(f"required file is missing: {relative}")

    attributes_path = ROOT / ".gitattributes"
    if attributes_path.is_file():
        attributes = attributes_path.read_text(encoding="utf-8").splitlines()
        if "* text=auto eol=lf" not in attributes:
            failures.append(
                ".gitattributes must keep cross-platform text checkouts on LF"
            )

    package_path = ROOT / "web" / "package.json"
    if package_path.is_file():
        package = json.loads(package_path.read_text(encoding="utf-8"))
        expected_scripts = {
            "esbuild@0.28.2": True,
            "unrs-resolver@1.12.2": True,
        }
        if package.get("allowScripts") != expected_scripts:
            failures.append("web install-script approvals must remain exact and version-pinned")

    release_gate = ROOT / "tools" / "release_gate.py"
    if release_gate.is_file():
        result = subprocess.run(
            [sys.executable, str(release_gate), "--self-test"],
            cwd=ROOT,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
        )
        if result.returncode:
            failures.append(f"release gate self-test failed: {(result.stderr or result.stdout).strip()}")
        elif result.stdout.strip():
            print(result.stdout.strip())

    try:
        git_root = command_version(
            ["git", "-c", f"safe.directory={ROOT}", "rev-parse", "--show-toplevel"]
        )
        if Path(git_root).resolve() != ROOT:
            failures.append(f"Git root mismatch: {git_root}")
    except (OSError, subprocess.SubprocessError, ValueError) as error:
        failures.append(f"Git repository unavailable: {error}")

    go = options.go
    if go is None:
        go = shutil.which("go")
        if go is None and (Path("C:/Program Files/Go/bin/go.exe")).is_file():
            go = "C:/Program Files/Go/bin/go.exe"
    if go is None:
        failures.append("Go is not installed or discoverable")
    else:
        try:
            minimum = required_go_version(ROOT)
            version = command_version([go, "version"])
            if not supported_go(version, minimum):
                required = ".".join(map(str, minimum))
                failures.append(
                    f"unsupported Go toolchain: {version}; go.mod requires {required} "
                    "or a newer patch in that minor release"
                )
            else:
                print(f"go: {version}")
        except (OSError, subprocess.SubprocessError, ValueError) as error:
            failures.append(f"Go toolchain could not be inspected ({go}): {error}")

    engines = json.loads(package_path.read_text(encoding="utf-8"))["engines"]
    for name in ("node", "npm"):
        executable = shutil.which(name)
        if executable is None:
            failures.append(f"{name} is not installed or discoverable")
            continue
        try:
            version = command_version([executable, "--version"])
        except (OSError, subprocess.SubprocessError, ValueError) as error:
            failures.append(f"{name} version could not be inspected: {error}")
            continue
        if not supported(version, engines[name]):
            failures.append(f"{name} {engines[name]} is required, found {version}")
        else:
            print(f"{name}: {version}")

    # Validate prerequisites before go run can fetch the already-pinned parser.
    # This also works before npm ci: workflow checks do not use web/node_modules.
    if failures:
        for failure in failures:
            print(f"ERROR: {failure}", file=sys.stderr)
        return 1
    failures.extend(workflow_problems(ROOT, go))

    for arguments in (
        [str(ROOT / "tools/check_repository.py")],
        ["-m", "unittest", "discover", "-s", "tools", "-p", "test_*.py"],
    ):
        if subprocess.run([sys.executable, *arguments], cwd=ROOT).returncode:
            failures.append(f"repository contract failed: {' '.join(arguments)}")

    skill_check = subprocess.run([sys.executable, ROOT / "tools" / "validate_skills.py"], cwd=ROOT)
    if skill_check.returncode:
        failures.append("repository skill validation failed")

    if failures:
        for failure in failures:
            print(f"ERROR: {failure}", file=sys.stderr)
        return 1
    print("doctor: repository structure and toolchain are healthy")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
