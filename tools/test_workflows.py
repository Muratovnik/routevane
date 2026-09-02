"""Exercise doctor's real workflow boundary without frontend dependencies."""

import contextlib
import io
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import doctor
from doctor import workflow_problems


ROOT = Path(__file__).resolve().parents[1]
PIN = "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1"


class WorkflowContracts(unittest.TestCase):
    def setUp(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        directory = tempfile.TemporaryDirectory(prefix="workflow contracts ", dir=scratch)
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.go = shutil.which("go") or "C:/Program Files/Go/bin/go.exe"
        self.env = {key: value for key, value in os.environ.items()
                    if key not in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR")}
        self.env["AGENTMEMORY_INJECT_CONTEXT"] = "false"
        for relative in ("go.mod", "go.sum", "tools/workflowcheck/main.go"):
            target = self.root / relative
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, target)
        self.write(".gitignore", "/.github/workflows/*.local.yml\n")
        self.write(".github/workflows/ci.yml", self.workflow(f'"uses": "{PIN}"'))
        self.write(".github/workflows/release.yml", self.workflow(f"uses: {PIN}"))
        subprocess.run(["git", "init", "--quiet"], cwd=self.root, env=self.env, check=True, capture_output=True)

    def write(self, relative, content):
        path = self.root / relative
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content, encoding="utf-8")

    def workflow(self, reference):
        return (
            "name: test\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n"
            f"      - {reference}\n"
            "      - run: python .github/relkit.pyz audit --history\n"
        )

    def check(self):
        with patch.dict(os.environ, self.env, clear=True):
            return workflow_problems(self.root, self.go)

    def test_quotes_comments_and_run_text_need_no_frontend_installation(self):
        text = self.workflow(f"uses: '{PIN}'")
        text += "      # uses: actions/checkout@v4\n      - run: |\n          echo 'uses: example@main'\n"
        self.write(".github/workflows/ci.yml", text)
        self.assertFalse((self.root / "web").exists())
        self.assertEqual(self.check(), [])

    def test_mutable_reference_with_quoted_key_fails_with_location(self):
        self.write(".github/workflows/ci.yml", self.workflow('"uses": actions/checkout@v4'))
        errors = self.check()
        self.assertTrue(any(".github/workflows/ci.yml:7:" in error and "full commit SHA" in error for error in errors), errors)

    def test_ignored_draft_is_excluded_but_force_added_draft_is_checked(self):
        path = ".github/workflows/draft.local.yml"
        self.write(path, self.workflow('"uses": actions/checkout@v4'))
        self.assertEqual(self.check(), [])
        subprocess.run(["git", "add", "-f", "--", path], cwd=self.root, env=self.env, check=True, capture_output=True)
        errors = self.check()
        self.assertTrue(any(path in error and "full commit SHA" in error for error in errors), errors)

    def test_ignored_required_workflow_cannot_substitute_for_published_source(self):
        self.write(".git/info/exclude", "/.github/workflows/ci.yml\n")
        self.assertIn(".github/workflows/ci.yml: required workflow is not publishable", self.check())

    def test_parse_failure_cannot_become_a_success(self):
        self.write(".github/workflows/ci.yml", "jobs: [\n")
        self.assertTrue(any("invalid workflow YAML" in error for error in self.check()))

    def test_unavailable_parser_cannot_become_a_success(self):
        errors = workflow_problems(self.root, str(self.root / "missing-go"))
        self.assertTrue(any("could not run" in error for error in errors), errors)

    def test_existing_public_history_requirement_is_retained(self):
        self.write(".github/workflows/ci.yml", self.workflow(f"uses: {PIN}").replace("audit --history", "audit"))
        self.assertIn(".github/workflows/ci.yml: public history gate is missing", self.check())

class PrerequisiteContracts(unittest.TestCase):
    def test_failed_prerequisite_stops_before_parser_or_dependency_using_tests(self):
        versions = {"git": str(ROOT), "go": "go version go1.27.0 windows/amd64",
                    "node": "v22.0.0", "npm": "11.17.0"}
        with (
            patch("doctor.shutil.which", side_effect=lambda name: name),
            patch("doctor.command_version", side_effect=lambda args: versions[args[0]]),
            patch("doctor.workflow_problems") as parser,
            patch("doctor.subprocess.run", return_value=subprocess.CompletedProcess([], 0, "", "")) as commands,
            contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()) as errors,
        ):
            self.assertEqual(doctor.main(), 1)
        self.assertIn("node", errors.getvalue())
        parser.assert_not_called()
        self.assertEqual(commands.call_count, 1)
        self.assertIn("--self-test", commands.call_args.args[0])


if __name__ == "__main__":
    unittest.main()
