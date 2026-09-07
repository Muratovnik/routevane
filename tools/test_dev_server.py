"""Development lifecycle regressions; live HMR/API acceptance is test:dev."""

import argparse
import ctypes
import os
import socket
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path

from dev_server import Session, backend_inputs, free_port, port_number


def wait_until(predicate, timeout: float = 10) -> bool:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return True
        time.sleep(0.05)
    return predicate()


def windows_process_is_running(pid: int) -> bool:
    kernel32 = ctypes.WinDLL("kernel32", use_last_error=True)
    kernel32.OpenProcess.argtypes = (ctypes.c_ulong, ctypes.c_int, ctypes.c_ulong)
    kernel32.OpenProcess.restype = ctypes.c_void_p
    kernel32.WaitForSingleObject.argtypes = (ctypes.c_void_p, ctypes.c_ulong)
    kernel32.WaitForSingleObject.restype = ctypes.c_ulong
    kernel32.CloseHandle.argtypes = (ctypes.c_void_p,)
    synchronize = 0x00100000
    wait_timeout = 0x00000102
    handle = kernel32.OpenProcess(synchronize, False, pid)
    if not handle:
        return False
    try:
        return kernel32.WaitForSingleObject(handle, 0) == wait_timeout
    finally:
        kernel32.CloseHandle(handle)


class DevelopmentTests(unittest.TestCase):
    def test_busy_ports_fail_without_reusing_or_stopping_the_listener(self):
        with socket.socket() as listener:
            listener.bind(("127.0.0.1", 0))
            listener.listen()
            with self.assertRaises(OSError):
                free_port(listener.getsockname()[1])
            self.assertGreater(listener.fileno(), 0)
        self.assertGreater(free_port(), 0)

    def test_watch_inputs_cover_add_change_delete_but_not_ui_output_or_data(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            catalog = root / "catalog"
            catalog.mkdir()
            source = root / "internal/sample.go"
            source.parent.mkdir()
            source.write_text("package sample", encoding="utf-8")
            before = backend_inputs(root, catalog)
            (source.parent / "ui").mkdir()
            (source.parent / "ui/generated.js").write_text("generated", encoding="utf-8")
            (root / "data.db").write_text("state", encoding="utf-8")
            self.assertEqual(before, backend_inputs(root, catalog))
            source.write_text("package renamed", encoding="utf-8")
            self.assertNotEqual(before, backend_inputs(root, catalog))
            source.unlink()
            self.assertNotIn(str(source), backend_inputs(root, catalog))
            target = catalog / "list.yaml"
            target.write_text("list", encoding="utf-8")
            self.assertIn(str(target), backend_inputs(root, catalog))

    def test_session_stops_only_the_child_it_owns(self):
        session = Session()
        child = session.spawn([sys.executable, "-c", "import time; time.sleep(60)"])
        session.close()
        self.assertIsNotNone(child.poll())
        self.assertEqual(session.children, [])

    @unittest.skipUnless(os.name == "nt", "Windows Job Object behavior")
    def test_failed_job_assignment_does_not_leave_an_unowned_child(self):
        class FailingJob:
            child = None

            def assign(self, child):
                self.child = child
                raise OSError("injected assignment failure")

            def close(self):
                pass

        session = Session()
        session._job.close()
        failing_job = FailingJob()
        session._job = failing_job
        try:
            with self.assertRaisesRegex(OSError, "injected assignment failure"):
                session.spawn([sys.executable, "-c", "import time; time.sleep(60)"])
            self.assertIsNotNone(failing_job.child)
            self.assertIsNotNone(failing_job.child.poll())
            self.assertEqual(session.children, [])
        finally:
            session.close()

    @unittest.skipUnless(os.name == "nt", "Windows Job Object behavior")
    def test_job_stops_child_when_only_the_supervisor_is_terminated(self):
        with tempfile.TemporaryDirectory() as directory:
            temporary = Path(directory)
            supervisor_ready = temporary / "supervisor.ready"
            child_ready = temporary / "child.ready"
            port = free_port()
            child_script = """
import os
import socket
import sys
import time
from pathlib import Path

listener = socket.socket()
listener.setsockopt(socket.SOL_SOCKET, socket.SO_EXCLUSIVEADDRUSE, 1)
listener.bind(("127.0.0.1", int(sys.argv[1])))
listener.listen()
Path(sys.argv[2]).write_text(str(os.getpid()), encoding="utf-8")
time.sleep(60)
"""
            supervisor_script = """
import subprocess
import sys
import time
from pathlib import Path

sys.path.insert(0, sys.argv[1])
from dev_server import Session

session = Session()
child = session.spawn(
    [sys.executable, "-c", sys.argv[2], sys.argv[3], sys.argv[4]],
    stdout=subprocess.DEVNULL,
    stderr=subprocess.DEVNULL,
)
Path(sys.argv[5]).write_text(str(child.pid), encoding="utf-8")
time.sleep(60)
"""
            supervisor = subprocess.Popen(
                [
                    sys.executable,
                    "-c",
                    supervisor_script,
                    str(Path(__file__).parent),
                    child_script,
                    str(port),
                    str(child_ready),
                    str(supervisor_ready),
                ],
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
            )
            child_pid = None
            try:
                self.assertTrue(
                    wait_until(lambda: supervisor_ready.is_file() or supervisor.poll() is not None),
                    "supervisor did not report its child",
                )
                if supervisor.poll() is not None:
                    stdout, stderr = supervisor.communicate()
                    self.fail(f"supervisor exited early: {stdout!r} {stderr!r}")
                child_pid = int(supervisor_ready.read_text(encoding="utf-8"))
                self.assertTrue(wait_until(child_ready.is_file), "child did not bind its port")
                with self.assertRaises(OSError):
                    free_port(port)

                # TerminateProcess targets this supervisor only. Closing its last
                # Job Object handle must terminate the separately grouped child.
                supervisor.kill()
                supervisor.wait(timeout=5)
                self.assertTrue(
                    wait_until(lambda: not windows_process_is_running(child_pid)),
                    f"owned child {child_pid} survived supervisor termination",
                )
                self.assertTrue(
                    wait_until(lambda: _port_is_bindable(port)),
                    f"owned child kept port {port} bound",
                )
            finally:
                if supervisor.poll() is None:
                    supervisor.kill()
                    supervisor.wait(timeout=5)
                supervisor.communicate(timeout=5)
                if child_pid is not None and windows_process_is_running(child_pid):
                    subprocess.run(
                        ["taskkill", "/PID", str(child_pid), "/T", "/F"],
                        check=False,
                        stdout=subprocess.DEVNULL,
                        stderr=subprocess.DEVNULL,
                        creationflags=subprocess.CREATE_NO_WINDOW,
                    )

    def test_ports_are_bounded(self):
        for value in ("-1", "65536"):
            with self.assertRaises(argparse.ArgumentTypeError):
                port_number(value)
        self.assertEqual(port_number("0"), 0)
        self.assertEqual(port_number("8765"), 8765)


def _port_is_bindable(port: int) -> bool:
    try:
        free_port(port)
        return True
    except OSError:
        return False
