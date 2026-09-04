#!/usr/bin/env python3
"""Run the existing Go executable and Nuxt HMR as one owned development session."""

from __future__ import annotations

import argparse
import ctypes
import http.client
import json
import os
import shutil
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import webbrowser
from pathlib import Path

if os.name == "nt":
    from ctypes import wintypes


ROOT = Path(__file__).resolve().parents[1]


if os.name == "nt":
    JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000
    JOB_OBJECT_EXTENDED_LIMIT_INFORMATION_CLASS = 9

    class _JobObjectBasicLimitInformation(ctypes.Structure):
        _fields_ = [
            ("PerProcessUserTimeLimit", ctypes.c_int64),
            ("PerJobUserTimeLimit", ctypes.c_int64),
            ("LimitFlags", wintypes.DWORD),
            ("MinimumWorkingSetSize", ctypes.c_size_t),
            ("MaximumWorkingSetSize", ctypes.c_size_t),
            ("ActiveProcessLimit", wintypes.DWORD),
            ("Affinity", ctypes.c_size_t),
            ("PriorityClass", wintypes.DWORD),
            ("SchedulingClass", wintypes.DWORD),
        ]

    class _IoCounters(ctypes.Structure):
        _fields_ = [
            ("ReadOperationCount", ctypes.c_uint64),
            ("WriteOperationCount", ctypes.c_uint64),
            ("OtherOperationCount", ctypes.c_uint64),
            ("ReadTransferCount", ctypes.c_uint64),
            ("WriteTransferCount", ctypes.c_uint64),
            ("OtherTransferCount", ctypes.c_uint64),
        ]

    class _JobObjectExtendedLimitInformation(ctypes.Structure):
        _fields_ = [
            ("BasicLimitInformation", _JobObjectBasicLimitInformation),
            ("IoInfo", _IoCounters),
            ("ProcessMemoryLimit", ctypes.c_size_t),
            ("JobMemoryLimit", ctypes.c_size_t),
            ("PeakProcessMemoryUsed", ctypes.c_size_t),
            ("PeakJobMemoryUsed", ctypes.c_size_t),
        ]

    _kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    _kernel32.CreateJobObjectW.argtypes = (ctypes.c_void_p, wintypes.LPCWSTR)
    _kernel32.CreateJobObjectW.restype = wintypes.HANDLE
    _kernel32.SetInformationJobObject.argtypes = (
        wintypes.HANDLE,
        ctypes.c_int,
        ctypes.c_void_p,
        wintypes.DWORD,
    )
    _kernel32.SetInformationJobObject.restype = wintypes.BOOL
    _kernel32.AssignProcessToJobObject.argtypes = (wintypes.HANDLE, wintypes.HANDLE)
    _kernel32.AssignProcessToJobObject.restype = wintypes.BOOL
    _kernel32.CloseHandle.argtypes = (wintypes.HANDLE,)
    _kernel32.CloseHandle.restype = wintypes.BOOL


    class _WindowsJob:
        """Own child process trees until this supervisor closes or exits."""

        def __init__(self) -> None:
            self.handle = _kernel32.CreateJobObjectW(None, None)
            if not self.handle:
                raise ctypes.WinError(ctypes.get_last_error())
            limits = _JobObjectExtendedLimitInformation()
            limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
            if not _kernel32.SetInformationJobObject(
                self.handle,
                JOB_OBJECT_EXTENDED_LIMIT_INFORMATION_CLASS,
                ctypes.byref(limits),
                ctypes.sizeof(limits),
            ):
                error = ctypes.WinError(ctypes.get_last_error())
                _kernel32.CloseHandle(self.handle)
                self.handle = None
                raise error

        def assign(self, child: subprocess.Popen) -> None:
            if self.handle is None:
                raise OSError("development process job is already closed")
            if not _kernel32.AssignProcessToJobObject(
                self.handle, int(child._handle)  # noqa: SLF001 - Win32 Popen handle
            ):
                raise ctypes.WinError(ctypes.get_last_error())

        def close(self) -> None:
            if self.handle is not None:
                _kernel32.CloseHandle(self.handle)
                self.handle = None


def free_port(port: int = 0) -> int:
    with socket.socket() as listener:
        if os.name == "nt":
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_EXCLUSIVEADDRUSE, 1)
        listener.bind(("127.0.0.1", port))
        return listener.getsockname()[1]


def backend_inputs(root: Path, catalog: Path) -> dict[str, tuple[int, int]]:
    paths = [root / "go.mod", root / "go.sum"]
    for directory in ("cmd", "internal", "sdk"):
        paths.extend((root / directory).rglob("*.go"))
    paths.extend(path for path in catalog.rglob("*") if path.is_file())
    result = {}
    for path in paths:
        try:
            stat = path.stat()
            result[str(path)] = (stat.st_mtime_ns, stat.st_size)
        except FileNotFoundError:
            pass  # An editor's atomic rename is picked up by the next sample.
    return result


class Session:
    def __init__(self) -> None:
        self.children: list[subprocess.Popen] = []
        self.stopping = threading.Event()
        self._job = _WindowsJob() if os.name == "nt" else None

    def spawn(self, command: list[str], **kwargs) -> subprocess.Popen:
        options = ({"creationflags": subprocess.CREATE_NEW_PROCESS_GROUP}
                   if os.name == "nt" else {"start_new_session": True})
        # Children must not consume the owner's stdin lifecycle pipe (or wait
        # for interactive terminal input in a background test session).
        child = subprocess.Popen(command, cwd=ROOT, stdin=subprocess.DEVNULL, **options, **kwargs)
        if self._job is not None:
            try:
                self._job.assign(child)
            except BaseException:
                subprocess.run(
                    ["taskkill", "/PID", str(child.pid), "/T", "/F"], check=False,
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                    creationflags=subprocess.CREATE_NO_WINDOW,
                )
                if child.poll() is None:
                    child.kill()
                child.wait(timeout=5)
                raise
        self.children.append(child)
        return child

    def stop(self, child: subprocess.Popen) -> None:
        if child.poll() is None:
            try:
                if os.name == "nt":
                    child.send_signal(signal.CTRL_BREAK_EVENT)
                else:
                    os.killpg(child.pid, signal.SIGINT)
                child.wait(timeout=5)
            except (OSError, subprocess.TimeoutExpired):
                if os.name == "nt":
                    subprocess.run(
                        ["taskkill", "/PID", str(child.pid), "/T", "/F"], check=False,
                        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                        creationflags=subprocess.CREATE_NO_WINDOW,
                    )
                else:
                    os.killpg(child.pid, signal.SIGKILL)
                child.wait(timeout=5)
        self.children.remove(child)

    def close(self) -> None:
        for child in self.children.copy()[::-1]:
            self.stop(child)
        if self._job is not None:
            self._job.close()

    def wait(self, seconds: float = 0.25) -> None:
        if self.stopping.wait(seconds):
            raise InterruptedError

    def build(self, go: str, destination: Path) -> bool:
        print("[dev] Building Go backend...", flush=True)
        child = self.spawn([go, "build", "-mod=readonly", "-o", str(destination), "./cmd/routing-agent"])
        while child.poll() is None:
            self.wait()
        self.children.remove(child)
        return child.returncode == 0

    def ready(self, child: subprocess.Popen, port: int, path: str, timeout: int = 90) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            if child.poll() is not None:
                raise RuntimeError(f"development process {child.pid} exited ({child.returncode})")
            connection = http.client.HTTPConnection("127.0.0.1", port, timeout=1)
            try:
                connection.request("GET", path)
                response = connection.getresponse()
                body = response.read(2 * 1024 * 1024)
                if response.status == 200 and (
                    json.loads(body).get("status") == "ok" if path == "/health" else b"/@vite/client" in body
                ):
                    return
            except (OSError, ValueError, http.client.HTTPException):
                pass
            finally:
                connection.close()
            self.wait()
        raise TimeoutError(f"development server was not ready at http://127.0.0.1:{port}{path}")


def run(args: argparse.Namespace, session: Session) -> None:
    go = args.go or shutil.which("go")
    if not go and os.name == "nt":
        fallback = Path("C:/Program Files/Go/bin/go.exe")
        go = str(fallback) if fallback.is_file() else None
    node = shutil.which("node")
    nuxt = ROOT / "web/node_modules/nuxt/bin/nuxt.mjs"
    if not go or not node or not nuxt.is_file():
        raise RuntimeError("Go, Node.js and web dependencies are required; run tools/dev.ps1 setup first")
    port = free_port(args.port)
    api_port = free_port(args.api_port)
    if port == api_port:
        raise ValueError("UI and API ports must differ")
    origin = f"http://127.0.0.1:{port}"
    api_origin = f"http://127.0.0.1:{api_port}"
    data = args.data_dir.resolve()
    catalog = args.catalog_dir.resolve()
    if not catalog.is_dir():
        raise ValueError(f"catalog directory is missing: {catalog}")
    scratch = ROOT / ".cache/dev"
    if not scratch.resolve().is_relative_to(ROOT.resolve()):
        raise ValueError("development scratch directory must stay inside the checkout")
    for ancestor in (scratch, scratch.parent):
        if ancestor.is_symlink() or (ancestor.exists() and getattr(ancestor.stat(), "st_file_attributes", 0) & 0x400):
            raise ValueError(f"linked development scratch directory: {ancestor}")
    scratch.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="session-", dir=scratch) as directory:
        binary_root = Path(directory)
        generation = 0
        backend = None

        def rebuild() -> bool:
            nonlocal generation, backend
            candidate = binary_root / f"routing-agent-{generation}{'.exe' if os.name == 'nt' else ''}"
            if not session.build(go, candidate):
                if backend is not None:
                    print("[dev] Go build failed; last working backend stays up. Save a Go file to retry.", flush=True)
                return False
            if backend is not None:
                session.stop(backend)
            backend = session.spawn([
                str(candidate), "serve", "--port", str(api_port),
                "--catalog-dir", str(catalog), "--data-dir", str(data),
            ])
            session.ready(backend, api_port, "/health")
            generation += 1
            print(f"[dev] Backend ready: {api_origin} (PID {backend.pid}, build {generation})", flush=True)
            for obsolete in binary_root.iterdir():
                if obsolete != candidate:
                    obsolete.unlink()
            return True

        try:
            observed = backend_inputs(ROOT, catalog)
            if not rebuild():
                raise RuntimeError("initial Go build failed")
            environment = {**os.environ, "ROUTEVANE_DEV_API_ORIGIN": api_origin,
                           "ROUTEVANE_DEV_UI_ORIGIN": origin, "NUXT_TELEMETRY_DISABLED": "1"}
            frontend = session.spawn([
                node, str(nuxt), "dev", str(ROOT / "web"), "--host", "127.0.0.1",
                "--port", str(port), "--no-fork", "--no-clear",
            ], env=environment)
            print(f"[dev] Starting Nuxt HMR (PID {frontend.pid})...", flush=True)
            session.ready(frontend, port, "/")
            session.ready(frontend, port, "/health")
            print(f"[dev] Ready: {origin} | Vue/CSS HMR + Go/catalog reload | data: {data}", flush=True)
            print(
                "[dev] Stop both servers with Ctrl+C; closing this terminal also stops them on Windows.",
                flush=True,
            )
            if not args.no_browser:
                webbrowser.open(origin)
            pending_since = None
            while True:
                session.wait()
                for child in (frontend, backend):
                    if child.poll() is not None:
                        raise RuntimeError(f"development process {child.pid} exited ({child.returncode})")
                current = backend_inputs(ROOT, catalog)
                if current != observed:
                    observed, pending_since = current, time.monotonic()
                elif pending_since is not None and time.monotonic() - pending_since >= 0.4:
                    pending_since = None
                    rebuild()
        finally:
            session.close()


def port_number(value: str) -> int:
    port = int(value)
    if not 0 <= port <= 65535:
        raise argparse.ArgumentTypeError("port must be between 0 and 65535")
    return port


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--port", type=port_number, default=8765, help="UI port (default: 8765)")
    parser.add_argument("--api-port", type=port_number, default=0, help="API port (default: a free loopback port)")
    parser.add_argument("--data-dir", type=Path, default=ROOT / ".cache/dev-data")
    parser.add_argument("--catalog-dir", type=Path, default=ROOT / "catalog")
    parser.add_argument("--go", help="Go executable, resolved by tools/dev.ps1")
    parser.add_argument("--no-browser", action="store_true")
    parser.add_argument("--stop-on-stdin-close", action="store_true", help="Stop when the owning test runner closes stdin")
    args = parser.parse_args()
    try:
        session = Session()
    except OSError as error:
        print(f"[dev] Could not create the Windows process job: {error}", file=sys.stderr, flush=True)
        return 1
    for signum in (signal.SIGINT, signal.SIGTERM):
        signal.signal(signum, lambda *_: session.stopping.set())
    if os.name == "nt":
        signal.signal(signal.SIGBREAK, lambda *_: session.stopping.set())
    if args.stop_on_stdin_close:
        def watch_owner() -> None:
            sys.stdin.buffer.read()
            session.stopping.set()
        threading.Thread(target=watch_owner, daemon=True).start()
    try:
        run(args, session)
        return 0
    except (InterruptedError, KeyboardInterrupt):
        return 0
    except (OSError, ValueError, RuntimeError, TimeoutError) as error:
        print(f"[dev] {error}", file=sys.stderr, flush=True)
        return 1
    finally:
        session.close()


if __name__ == "__main__":
    raise SystemExit(main())
