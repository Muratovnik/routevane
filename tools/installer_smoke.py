#!/usr/bin/env python3
"""Install the built Windows installer silently, inspect it, and uninstall it.

The installer carries the production identity, so on a machine where Routevane
is installed it would replace that installation and its uninstaller would then
remove the operator's shortcuts and registration. The smoke therefore refuses
to run while any Routevane is registered; it is meant for a clean release runner.
"""

from __future__ import annotations

import argparse
import itertools
import os
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path
from typing import Callable


ROOT = Path(__file__).resolve().parents[1]
STABLE_VERSION = re.compile(r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)")
# The updater must keep asking this repository's GitHub releases; any other
# feed in a shipped installer is a release defect, not a configuration choice.
EXPECTED_UPDATE_FEED = {"provider": "github", "owner": "Muratovnik", "repo": "routevane"}
INSTALL_TIMEOUT_SECONDS = 300
# A one-click NSIS installer can return before the last file is visible, and
# an NSIS uninstaller re-launches itself from a temporary copy and returns at
# once. Both are therefore observed on disk, not trusted by exit code alone.
INSTALL_DEADLINE_SECONDS = 60.0
UNINSTALL_DEADLINE_SECONDS = 120.0
UNINSTALL_REGISTRY_KEY = r"Software\Microsoft\Windows\CurrentVersion\Uninstall"


def require_windows_x64() -> None:
    if os.name != "nt" or platform.machine().lower() not in ("amd64", "x86_64"):
        raise ValueError("the desktop installer smoke requires Windows x64")


def installed_products() -> list[str]:
    """Name every registered Routevane installation, per-user or per-machine."""
    import winreg

    names: list[str] = []
    for hive in (winreg.HKEY_CURRENT_USER, winreg.HKEY_LOCAL_MACHINE):
        try:
            registry = winreg.OpenKey(hive, UNINSTALL_REGISTRY_KEY)
        except OSError:
            continue
        with registry:
            for index in itertools.count():
                try:
                    identity = winreg.EnumKey(registry, index)
                except OSError:
                    break
                try:
                    with winreg.OpenKey(registry, identity) as entry:
                        name = winreg.QueryValueEx(entry, "DisplayName")[0]
                except OSError:
                    continue
                if isinstance(name, str) and re.fullmatch(r"Routevane \d+\.\d+\.\d+", name):
                    names.append(name)
    return sorted(names)


def installer_for(assets: Path, version: str) -> Path:
    if not STABLE_VERSION.fullmatch(version):
        raise ValueError("the installer smoke requires a stable vX.Y.Z version")
    # electron-builder names the installer from the bare version.
    return assets / f"Routevane-{version[1:]}-x64-setup.exe"


def installer_command(installer: Path, install_dir: Path) -> str:
    # NSIS takes /D= verbatim up to the end of the command line and refuses a
    # quoted value, so the command line is written out rather than built from a
    # list, which would quote a destination containing spaces.
    return f'"{installer}" /S /D={install_dir}'


def run_installer(installer: Path, install_dir: Path) -> None:
    subprocess.run(installer_command(installer, install_dir), check=True, timeout=INSTALL_TIMEOUT_SECONDS,
                   stdin=subprocess.DEVNULL, creationflags=subprocess.CREATE_NO_WINDOW)


def run_uninstaller(uninstaller: Path) -> None:
    subprocess.run([str(uninstaller), "/S"], check=True, timeout=INSTALL_TIMEOUT_SECONDS,
                   stdin=subprocess.DEVNULL, creationflags=subprocess.CREATE_NO_WINDOW)


def reported_version(binary: Path) -> str:
    return subprocess.run([str(binary), "version"], check=True, capture_output=True, text=True, timeout=30,
                          stdin=subprocess.DEVNULL).stdout.strip()


def read_update_config(path: Path) -> dict[str, str]:
    """Read the flat key/value document electron-builder writes for the updater."""
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        if not line.strip() or line.lstrip().startswith("#") or ":" not in line:
            continue
        key, value = line.split(":", 1)
        values[key.strip()] = value.strip().strip("'\"")
    return values


def installation_problems(install_dir: Path, version: str) -> list[str]:
    resources = install_dir / "resources"
    problems: list[str] = []
    core = resources / "routevane.exe"
    if not core.is_file():
        return [f"installed core is missing: {core}"]
    reported = reported_version(core)
    if reported != f"routevane {version}":
        problems.append(f"installed core reports {reported!r}, expected 'routevane {version}'")
    if not (resources / "desktop-installed").is_file():
        problems.append("installer marker resources/desktop-installed is missing")
    update_config = resources / "app-update.yml"
    if not update_config.is_file():
        problems.append("updater configuration resources/app-update.yml is missing")
    else:
        feed = read_update_config(update_config)
        for key, expected in EXPECTED_UPDATE_FEED.items():
            if feed.get(key) != expected:
                problems.append(f"updater {key} is {feed.get(key)!r}, expected {expected!r}")
    return problems


def find_uninstaller(install_dir: Path) -> Path | None:
    if not install_dir.is_dir():
        return None
    for candidate in sorted(install_dir.iterdir()):
        if candidate.is_file() and re.fullmatch(r"uninstall.*\.exe", candidate.name, re.IGNORECASE):
            return candidate
    return None


def wait_until(condition: Callable[[], bool], deadline_seconds: float) -> bool:
    limit = time.monotonic() + deadline_seconds
    while True:
        if condition():
            return True
        if time.monotonic() >= limit:
            return False
        time.sleep(0.25)


def owned_scratch(scratch: Path) -> Path:
    for item in (scratch, *scratch.parents):
        if item.is_symlink() or (item.exists() and getattr(item.stat(), "st_file_attributes", 0) & 0x400):
            raise ValueError(f"linked smoke scratch directory: {item}")
    scratch.mkdir(parents=True, exist_ok=True)
    return Path(tempfile.mkdtemp(prefix="run-", dir=scratch)).resolve()


def smoke(installer: Path, version: str, scratch: Path | None = None) -> None:
    require_windows_x64()
    if installer.name != installer_for(installer.parent, version).name or not installer.is_file():
        raise ValueError(f"installer for {version} is missing: {installer}")
    present = installed_products()
    if present:
        raise ValueError("Routevane is already installed here (" + ", ".join(present) + "); "
                         "the smoke would replace it, so run it on a clean machine")
    run_dir = owned_scratch(scratch if scratch is not None else ROOT / "tmp/installer-smoke")
    install_dir = run_dir / "installed"
    core = install_dir / "resources" / "routevane.exe"
    problems: list[str] = []
    removed = False
    try:
        run_installer(installer.resolve(), install_dir)
        if not wait_until(core.is_file, INSTALL_DEADLINE_SECONDS):
            problems.append(f"installer did not place the core at {core}")
        else:
            problems.extend(installation_problems(install_dir, version))
    finally:
        # The run owns this installation, so it is uninstalled whatever the
        # checks found. An uninstall that fails is itself a release defect, and
        # what it left behind stays in the run directory as evidence.
        uninstaller = find_uninstaller(install_dir)
        if uninstaller is None:
            removed = not install_dir.exists()
            if not removed:
                problems.append("the installation carries no uninstaller")
        else:
            run_uninstaller(uninstaller)
            removed = wait_until(lambda: not core.exists() and not uninstaller.exists() and not installed_products(),
                                 UNINSTALL_DEADLINE_SECONDS)
            if not removed:
                problems.append("the silent uninstall left the installation behind")
        if removed:
            shutil.rmtree(run_dir)
    if problems:
        kept = "" if removed else f" (left in {run_dir})"
        raise ValueError("; ".join(problems) + kept)
    print(f"installer: {installer.name} installed routevane {version} with its marker and GitHub update feed, then uninstalled")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--assets", type=Path, required=True, help="Directory holding the built desktop release")
    parser.add_argument("--version", required=True)
    parser.add_argument("--temp-dir", type=Path, help="Owned scratch directory (default: project tmp/installer-smoke)")
    arguments = parser.parse_args(argv)
    try:
        smoke(installer_for(arguments.assets, arguments.version).resolve(), arguments.version, arguments.temp_dir)
    except (ValueError, OSError, subprocess.SubprocessError) as error:
        print(f"installer smoke failed: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
