"""Development lifecycle regressions; live HMR/API acceptance is test:dev."""

import argparse
import socket
import sys
import tempfile
import unittest
from pathlib import Path

from dev_server import Session, backend_inputs, free_port, port_number


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
            target = catalog / "service.yaml"
            target.write_text("service", encoding="utf-8")
            self.assertIn(str(target), backend_inputs(root, catalog))

    def test_session_stops_only_the_child_it_owns(self):
        session = Session()
        child = session.spawn([sys.executable, "-c", "import time; time.sleep(60)"])
        session.close()
        self.assertIsNotNone(child.poll())
        self.assertEqual(session.children, [])

    def test_ports_are_bounded(self):
        for value in ("-1", "65536"):
            with self.assertRaises(argparse.ArgumentTypeError):
                port_number(value)
        self.assertEqual(port_number("0"), 0)
        self.assertEqual(port_number("8765"), 8765)
