"""Prerequisite diagnostics must inspect the chosen toolchain before heavy gates."""

import contextlib
import io
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import doctor


ROOT = Path(__file__).resolve().parents[1]


class DoctorPrerequisites(unittest.TestCase):
    def test_go_minor_and_minimum_patch_come_from_the_module(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=scratch) as directory:
            root = Path(directory)
            (root / "go.mod").write_text("module example.test/check\n\ngo 1.27.2\n", encoding="utf-8")
            minimum = doctor.required_go_version(root)
            for version, allowed in (
                ("go version go1.27.1 linux/amd64", False),
                ("go version go1.27.2 linux/amd64", True),
                ("go version go1.27.9 windows/amd64", True),
                ("go version go1.28.0 linux/amd64", False),
                ("go version go1.27.2rc1 linux/amd64", False),
            ):
                with self.subTest(version=version):
                    self.assertEqual(doctor.supported_go(version, minimum), allowed)
            (root / "go.mod").write_text("module example.test/check\n\ngo 1.28.3\n", encoding="utf-8")
            self.assertFalse(doctor.supported_go("go version go1.28.2 linux/amd64", doctor.required_go_version(root)))
            self.assertTrue(doctor.supported_go("go version go1.28.3 linux/amd64", doctor.required_go_version(root)))

    def test_explicit_go_is_the_executable_validated_and_passed_to_workflow_checks(self):
        selected = "/selected toolchain/bin/go"
        minimum = ".".join(map(str, doctor.required_go_version(ROOT)))
        versions = {"git": str(ROOT), selected: f"go version go{minimum} linux/amd64",
                    "node": "v24.21.0", "npm": "11.19.0"}
        with (
            patch("doctor.shutil.which", side_effect=lambda name: name),
            patch("doctor.command_version", side_effect=lambda args: versions[args[0]]) as version,
            patch("doctor.workflow_problems", return_value=[]) as workflows,
            patch("doctor.subprocess.run", return_value=subprocess.CompletedProcess([], 0, "", "")),
            contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()),
        ):
            self.assertEqual(doctor.main(["--go", selected]), 0)
        version.assert_any_call([selected, "version"])
        workflows.assert_called_once_with(ROOT, selected)
        self.assertNotIn(["go", "version"], [call.args[0] for call in version.call_args_list])

    def test_failed_selected_go_reports_diagnostic_without_falling_back(self):
        selected = "/missing-selected-go"

        def version(arguments):
            if arguments[0] == selected:
                raise FileNotFoundError("selected executable is missing")
            return {"git": str(ROOT), "node": "v24.21.0", "npm": "11.19.0"}[arguments[0]]

        with (
            patch("doctor.shutil.which", side_effect=lambda name: name),
            patch("doctor.command_version", side_effect=version),
            patch("doctor.workflow_problems") as workflows,
            patch("doctor.subprocess.run", return_value=subprocess.CompletedProcess([], 0, "", "")),
            contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()) as errors,
        ):
            self.assertEqual(doctor.main(["--go", selected]), 1)
        self.assertIn("Go toolchain could not be inspected", errors.getvalue())
        self.assertIn(selected, errors.getvalue())
        workflows.assert_not_called()

    def test_declared_npm_range_accepts_npm_12_and_refuses_untested_lines(self):
        minimum = ".".join(map(str, doctor.required_go_version(ROOT)))
        for npm, allowed in (("11.16.9", False), ("11.17.0", True), ("12.2.0", True), ("13.0.0", False)):
            versions = {"git": str(ROOT), "go": f"go version go{minimum} linux/amd64",
                        "node": "v24.21.0", "npm": npm}
            with (
                self.subTest(npm=npm),
                patch("doctor.shutil.which", side_effect=lambda name: name),
                patch("doctor.command_version", side_effect=lambda args: versions[args[0]]),
                patch("doctor.workflow_problems", return_value=[]),
                patch("doctor.subprocess.run", return_value=subprocess.CompletedProcess([], 0, "", "")),
                contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()) as errors,
            ):
                self.assertEqual(doctor.main(["--go", "go"]), 0 if allowed else 1)
                self.assertEqual("npm" in errors.getvalue(), not allowed, errors.getvalue())

    def test_empty_version_is_a_clear_failure(self):
        with self.assertRaisesRegex(ValueError, "did not report a version"):
            doctor.command_version([sys.executable, "-c", "pass"])

    def test_version_command_cannot_wait_without_a_deadline(self):
        with self.assertRaises(subprocess.TimeoutExpired):
            doctor.command_version([sys.executable, "-c", "import time; time.sleep(60)"], timeout=0.1)


if __name__ == "__main__":
    unittest.main()
