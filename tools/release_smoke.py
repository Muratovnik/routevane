#!/usr/bin/env python3
"""Exercise the shipped native launcher against isolated fresh runtime state."""

from __future__ import annotations

import argparse
import hashlib
import http.client
import json
import os
import platform
import signal
import socket
import stat
import subprocess
import tempfile
import time
import zipfile
from pathlib import Path


def request(port: int, path: str) -> tuple[dict[str, str], bytes]:
    connection = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
    try:
        connection.request("GET", path)
        response = connection.getresponse()
        body = response.read(2 * 1024 * 1024 + 1)
        if response.status != 200 or len(body) > 2 * 1024 * 1024:
            raise ValueError(f"unexpected response for {path}: {response.status}")
        return dict(response.getheaders()), body
    finally:
        connection.close()


def stop(process: subprocess.Popen) -> None:
    if os.name == "nt":
        if process.poll() is None:
            subprocess.run(
                ["taskkill", "/PID", str(process.pid), "/T", "/F"],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False,
                creationflags=subprocess.CREATE_NO_WINDOW,
            )
    else:
        try:
            os.killpg(process.pid, signal.SIGINT)
        except ProcessLookupError:
            pass
    try:
        process.wait(timeout=8)
    except subprocess.TimeoutExpired:
        if os.name != "nt":
            os.killpg(process.pid, signal.SIGKILL)
        process.kill()
        process.wait(timeout=5)


def smoke(archive_path: Path, version: str, target: str) -> None:
    machine = platform.machine().lower()
    arch = "arm64" if machine in ("arm64", "aarch64") else "amd64"
    system = {"Windows": "windows", "Linux": "linux", "Darwin": "darwin"}[platform.system()]
    if target != f"{system}-{arch}":
        raise ValueError(f"native smoke requested {target} on {system}-{arch}")
    # Spaces exercise the same quoting the user's extracted folder may require.
    with tempfile.TemporaryDirectory(prefix="routevane release smoke ") as directory:
        root = Path(directory).resolve()
        with zipfile.ZipFile(archive_path) as archive:
            for entry in archive.infolist():
                destination = (root / entry.filename).resolve()
                if not destination.is_relative_to(root) or stat.S_ISLNK(entry.external_attr >> 16):
                    raise ValueError("unsafe archive entry")
                archive.extract(entry, root)
                if os.name != "nt" and not entry.is_dir():
                    destination.chmod(stat.S_IMODE(entry.external_attr >> 16))
        folders = list(root.iterdir())
        if len(folders) != 1 or not folders[0].is_dir():
            raise ValueError("archive must contain one product directory")
        product = folders[0]
        binary = product / ("routing-agent.exe" if os.name == "nt" else "routing-agent")
        reported = subprocess.check_output([str(binary), "version"], text=True).strip()
        if reported != f"routevane {version}":
            raise ValueError(f"wrong version: {reported}")
        if not (product / "README.txt").is_file():
            raise ValueError("offline instructions missing")
        with socket.socket() as reserve:
            reserve.bind(("127.0.0.1", 0))
            port = reserve.getsockname()[1]
        environment = {**os.environ, "ROUTEVANE_NO_BROWSER": "1"}
        environment.pop("ROUTEVANE_PLUGINS_DIR", None)
        if os.name == "nt":
            command = [os.environ.get("COMSPEC", "cmd.exe"), "/d", "/c", str(product / "start-routevane.cmd"), str(port)]
            options = {"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP | subprocess.CREATE_NO_WINDOW}
        else:
            command = ["sh", str(product / "start-routevane.sh"), str(port)]
            options = {"start_new_session": True}
        with (root / "startup.log").open("wb") as output:
            process = subprocess.Popen(command, cwd=product, env=environment, stdout=output, stderr=subprocess.STDOUT, **options)
            try:
                deadline = time.monotonic() + 20
                while True:
                    if process.poll() is not None:
                        raise ValueError("launcher exited before readiness")
                    try:
                        _, health = request(port, "/health")
                        if json.loads(health).get("status") == "ok":
                            break
                    except (OSError, ValueError, http.client.HTTPException):
                        pass
                    if time.monotonic() > deadline:
                        raise TimeoutError("launcher did not become ready")
                    time.sleep(0.1)
                headers, document = request(port, "/")
                normalized = {key.lower(): value for key, value in headers.items()}
                digest = normalized.get("x-routevane-ui-digest", "")
                if not digest.startswith("sha256-") or len(digest) != 71 or b"<html" not in document.lower():
                    raise ValueError("embedded UI identity is missing")
                _, catalog = request(port, "/v1/services")
                if not json.loads(catalog):
                    raise ValueError("catalog is empty")
                if not (product / "data/routing-agent.db").is_file():
                    raise ValueError("runtime data was not created in the documented directory")
                print(f"native launcher: {target}, {reported}, health=ok, UI={digest}, catalog=ok")
            finally:
                stop(process)
        with socket.socket() as probe:
            probe.settimeout(1)
            if probe.connect_ex(("127.0.0.1", port)) == 0:
                raise ValueError("launcher left its listener running")
    digest = hashlib.sha256(archive_path.read_bytes()).hexdigest()
    print(f"archive SHA256: {digest}; task-owned process and data cleaned")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--platform", required=True)
    arguments = parser.parse_args()
    smoke(arguments.archive.resolve(strict=True), arguments.version, arguments.platform)
