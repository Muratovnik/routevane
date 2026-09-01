"""Allow one pinned Chromium binary to create a sandbox on a disposable CI VM."""

from __future__ import annotations

import os
import re
import subprocess
import sys
from pathlib import Path, PurePosixPath


ROOT = Path(__file__).resolve().parents[1]


def sandbox_profile(browser: str, workspace: str, run: str) -> str:
    # AppArmor treats glob characters and variable expansions as policy, even
    # inside quotes. Accept a literal path only; spaces are ordinary path bytes.
    if not re.fullmatch(r"/[A-Za-z0-9_./ -]+", browser):
        raise ValueError("sandbox executable must be a literal absolute path")
    path, root = PurePosixPath(browser), PurePosixPath(workspace)
    if (
        not root.is_absolute()
        or ".." in path.parts
        or ".." in root.parts
        or not path.is_relative_to(root / ".cache/browsers")
        or path.name != "chrome"
        or not re.fullmatch(r"[0-9]+-[0-9]+", run)
    ):
        raise ValueError("sandbox profile escaped its CI browser or run")
    return (
        "abi <abi/4.0>,\n"
        f'profile routevane-ci-{run} "{path}" flags=(unconfined) {{\n'
        "  userns,\n"
        "}\n"
    )


def main() -> None:
    if (
        sys.platform != "linux"
        or os.environ.get("GITHUB_ACTIONS") != "true"
        or os.environ.get("RUNNER_ENVIRONMENT") != "github-hosted"
    ):
        raise SystemExit("sandbox setup is restricted to disposable GitHub-hosted Linux runners")
    workspace = Path(os.environ["GITHUB_WORKSPACE"]).resolve(strict=True)
    if workspace != ROOT:
        raise SystemExit("sandbox setup must run in the checked-out workspace")
    browser = subprocess.check_output(
        ["node", "-e", 'console.log(require("playwright-core").chromium.executablePath())'],
        cwd=ROOT / "web",
        env={**os.environ, "PLAYWRIGHT_BROWSERS_PATH": str(ROOT / ".cache/browsers")},
        text=True,
    ).strip()
    executable = Path(browser).resolve(strict=True)
    if not executable.is_file():
        raise SystemExit("pinned Chromium is not a regular file")
    run = f'{os.environ["GITHUB_RUN_ID"]}-{os.environ["GITHUB_RUN_ATTEMPT"]}'
    profile = sandbox_profile(executable.as_posix(), workspace.as_posix(), run)
    # Add (never replace) a run-owned rule. No policy file or cache persists;
    # the rule disappears with this job's VM. AppArmor and Chromium stay enabled.
    subprocess.run(
        ["sudo", "-n", "apparmor_parser", "--add", "--skip-cache"],
        input=profile, text=True, check=True,
    )
    print(f"Chromium sandbox user namespaces allowed for {executable}")


if __name__ == "__main__":
    main()
