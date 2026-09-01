#!/usr/bin/env python3
"""Standard-library structural diagnostics for the Routevane repository."""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path

from check_repository import supported


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
    "dev.cmd",
)
PINNED_ACTION = re.compile(
    r"^\s*-?\s*uses:\s+[\w.-]+/[\w.-]+@[0-9a-f]{40}(?:\s+#.*)?$"
)
RETIRED_PROJECT_CLIENT_SURFACE = (
    ".claude/settings.json",
    ".claude/settings.local.json",
    ".codex/config.toml",
    ".codex/workflow.toml",
)


def command_version(command: list[str]) -> str:
    result = subprocess.run(
        command,
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return (result.stdout or result.stderr).strip().splitlines()[0]


def main() -> int:
    failures: list[str] = []
    for relative in REQUIRED:
        if not (ROOT / relative).is_file():
            failures.append(f"required file is missing: {relative}")

    for relative in RETIRED_PROJECT_CLIENT_SURFACE:
        path = ROOT / relative
        if os.path.lexists(path):
            failures.append(
                f"retired project client configuration is present: {relative}; "
                "use the user-scoped registration instead"
            )

    attributes_path = ROOT / ".gitattributes"
    if attributes_path.is_file():
        attributes = attributes_path.read_text(encoding="utf-8").splitlines()
        if "* text=auto eol=lf" not in attributes:
            failures.append(
                ".gitattributes must keep cross-platform text checkouts on LF"
            )

    for path in sorted((ROOT / "docs").rglob("*.md")):
        if path.name == "README.md":
            continue
        text = path.read_text(encoding="utf-8")
        header = text.split("---", 2)
        if len(header) < 3 or not re.search(r"(?m)^status: (draft|adopted|superseded)$", header[1]):
            failures.append(f"{path.relative_to(ROOT)}: missing valid status frontmatter")

    if not any(os.path.lexists(ROOT / path) for path in RETIRED_PROJECT_CLIENT_SURFACE):
        print("project client configuration: absent (user-scoped services only)")

    package_path = ROOT / "web" / "package.json"
    if package_path.is_file():
        package = json.loads(package_path.read_text(encoding="utf-8"))
        expected_scripts = {
            "esbuild@0.28.2": True,
            "unrs-resolver@1.12.2": True,
        }
        if package.get("allowScripts") != expected_scripts:
            failures.append("web install-script approvals must remain exact and version-pinned")

    workflow_root = ROOT / ".github" / "workflows"
    for workflow_path in sorted(workflow_root.glob("*.y*ml")):
        workflow_text = workflow_path.read_text(encoding="utf-8")
        for number, line in enumerate(workflow_text.splitlines(), 1):
            if "uses:" in line and not PINNED_ACTION.fullmatch(line):
                failures.append(
                    f"{workflow_path.relative_to(ROOT)}:{number}: action must be pinned "
                    "to a full commit SHA"
                )
        if "audit --history --owner" in workflow_text:
            failures.append(
                f"{workflow_path.relative_to(ROOT)}: owner policy requires a private "
                "maintainer checkout and must not run in public CI"
            )

    for workflow_name in ("ci.yml", "release.yml"):
        workflow_path = workflow_root / workflow_name
        workflow_text = (
            workflow_path.read_text(encoding="utf-8") if workflow_path.is_file() else ""
        )
        if (
            workflow_path.is_file()
            and "python .github/relkit.pyz audit --history" not in workflow_text
        ):
            failures.append(f".github/workflows/{workflow_name}: public history gate is missing")

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
    except (OSError, subprocess.CalledProcessError) as error:
        failures.append(f"Git repository unavailable: {error}")

    go = shutil.which("go")
    if go is None and (Path("C:/Program Files/Go/bin/go.exe")).is_file():
        go = "C:/Program Files/Go/bin/go.exe"
    if go is None:
        failures.append("Go is not installed or discoverable")
    else:
        version = command_version([go, "version"])
        if not re.search(r"\bgo1\.27\.\d+\b", version):
            failures.append(f"unsupported Go toolchain: {version}")
        else:
            print(f"go: {version}")

    engines = json.loads(package_path.read_text(encoding="utf-8"))["engines"]
    for name in ("node", "npm"):
        executable = shutil.which(name)
        if executable is None:
            failures.append(f"{name} is not installed or discoverable")
            continue
        version = command_version([executable, "--version"])
        if not supported(version, engines[name]):
            failures.append(f"{name} {engines[name]} is required, found {version}")
        else:
            print(f"{name}: {version}")

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
    raise SystemExit(main())
