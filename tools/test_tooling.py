"""Behavioral regression tests for documentation and archive contracts."""

import json
import os
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path

from check_repository import (
    documentation_problems, publication_path_problems, scanner_problems, supported,
)
from ci_browser_sandbox import sandbox_profile
from release_archive import pack


class RepositoryContracts(unittest.TestCase):
    def test_ci_sandbox_profile_is_literal_and_scoped(self):
        root = "/work/source tree"
        browser = root + "/.cache/browsers/chromium-1234/chrome-linux64/chrome"
        profile = sandbox_profile(browser, root, "123-1")
        self.assertIn(f'"{browser}"', profile)
        self.assertIn("userns,", profile)
        for rejected in (
            "/outside/chrome", browser.replace(".cache", ".cache-evil"),
            browser.replace("1234", "*"), browser.replace("1234", "@{HOME}"),
            browser + "\n", browser + '"', browser + "/../chrome", browser + "-other",
        ):
            with self.subTest(path=rejected), self.assertRaises(ValueError):
                sandbox_profile(rejected, root, "123-1")
        with self.assertRaises(ValueError):
            sandbox_profile(browser, root, "123} profile injected")

    def test_fixture_exception_requires_both_path_and_content(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            config = '[[allowlists]]\npaths = ["fixture"]\nregexes = ["example"]\n'
            (root / ".betterleaks.toml").write_text(config, encoding="utf-8")
            self.assertEqual(len(scanner_problems(root)), 1)
            (root / ".betterleaks.toml").write_text(config + 'condition = "AND"\n', encoding="utf-8")
            self.assertEqual(scanner_problems(root), [])

    def test_version_range_checks_patch_and_upper_bound(self):
        for version in ("24.19.0", "v24.20.1"):
            self.assertTrue(supported(version, ">=24.19.0 <25"))
        for version in ("22.12.0", "24.18.9", "25.0.0", "24.19"):
            self.assertFalse(supported(version, ">=24.19.0 <25"))

    def test_links_require_a_real_reachable_target(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / "docs").mkdir()
            (root / "README.md").write_text("[Guide](docs/guide.md)", encoding="utf-8")
            (root / "docs/guide.md").write_text("[Other](missing.md)", encoding="utf-8")
            (root / "docs/orphan.md").write_text("unused", encoding="utf-8")
            paths = ["README.md", "docs/guide.md", "docs/orphan.md"]
            problems = documentation_problems(root, paths)
            self.assertTrue(any("missing local link" in p for p in problems))
            self.assertTrue(any("orphan.md" in p for p in problems))
            (root / "docs/guide.md").write_text("[Other](orphan.md)", encoding="utf-8")
            self.assertEqual(documentation_problems(root, paths), [])

    def test_withdrawn_working_document_paths_are_rejected(self):
        for relative in (
            "docs/history/implementation-plan.md", "docs/history/list-outputs-2026-08-22.md",
            "DOCS/HiStOrY/ui-audit-2026-08-21.md", "docs/history/ui-redesign-2026-08-21.md",
            "docs/plans/routing-service-implementation-plan.md", "docs/plans/2026-08-22-list-outputs-brief.md",
            "docs/plans/2026-08-21-ui-redesign-brief.md", "docs/audits/2026-08-21-ui-critique.md",
        ):
            with self.subTest(path=relative), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                source = root / relative
                source.parent.mkdir(parents=True)
                source.write_bytes(b"fixture")
                problems = publication_path_problems(root, [relative])
                self.assertEqual(len(problems), 1)
                self.assertIn("retired working-document path", problems[0])
                source.unlink()
                self.assertEqual(publication_path_problems(root, [relative]), [])

    def test_public_decisions_and_similar_directory_names_are_not_retired(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            paths = [
                "docs/adr/0001-audit-trail.md", "docs/historical-formats.md",
                "docs/plans-guide/example.txt", "docs/audits-format.md",
            ]
            for relative in paths:
                source = root / relative
                source.parent.mkdir(parents=True, exist_ok=True)
                source.write_text("fixture", encoding="utf-8")
            self.assertEqual(publication_path_problems(root, paths), [])

    def test_links_and_images_cannot_depend_on_unpublished_local_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / ".private").mkdir()
            (root / ".private/audit.md").write_text("private", encoding="utf-8")
            (root / ".private/screenshot.png").write_bytes(b"fixture")
            readme = root / "README.md"
            for target in (
                "[Local](.private/audit.md)", "[Local](.private/)",
                "![Screenshot](.private/screenshot.png)",
                "[Local](%2Eprivate/audit.md)",
            ):
                with self.subTest(link=target):
                    readme.write_text(target, encoding="utf-8")
                    problems = documentation_problems(root, ["README.md"])
                    self.assertEqual(len(problems), 1)
                    self.assertIn("local link target is not publishable", problems[0])
            (root / "examples").mkdir()
            (root / "examples/config.json").write_text("{}", encoding="utf-8")
            readme.write_text("[Examples](examples/) [Root](./)", encoding="utf-8")
            self.assertEqual(documentation_problems(root, ["README.md", "examples/config.json"]), [])

    def test_repository_command_checks_candidates_without_rejecting_old_index_entries(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in ("tools", "web", "docs/history", "docs/adr", ".private"):
                (root / name).mkdir(parents=True, exist_ok=True)
            shutil.copyfile(Path(__file__).with_name("check_repository.py"), root / "tools/check_repository.py")
            engines = {"node": ">=24.19.0 <25", "npm": ">=11.17.0 <12"}
            (root / "web/package.json").write_text(
                json.dumps({"engines": engines, "packageManager": "npm@11.17.0"}), encoding="utf-8",
            )
            (root / "web/package-lock.json").write_text(
                json.dumps({"packages": {"": {"engines": engines}}}), encoding="utf-8",
            )
            (root / ".node-version").write_text("24.19.0\n", encoding="utf-8")
            (root / ".betterleaks.toml").write_text("", encoding="utf-8")
            (root / ".gitignore").write_text(".private/\n", encoding="utf-8")
            (root / "docs/adr/README.md").write_text("# Decisions\n", encoding="utf-8")
            report_relative = "docs/history/ui-audit-2026-08-21.md"
            report = root / report_relative
            report.write_text("---\nstatus: superseded\n---\n# Internal report\n", encoding="utf-8")
            readme = root / "README.md"
            readme.write_text(f"[Decisions](docs/adr/README.md) [Report]({report_relative})", encoding="utf-8")
            env = {key: value for key, value in os.environ.items()
                   if key not in ("GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_PREFIX", "GIT_COMMON_DIR")}
            env["PYTHONIOENCODING"] = "utf-8"

            def run(*command):
                return subprocess.run(command, cwd=root, env=env, capture_output=True, encoding="utf-8")

            self.assertEqual(run("git", "init", "--quiet").returncode, 0)
            for tracked in (False, True):
                with self.subTest(tracked=tracked):
                    if tracked:
                        self.assertEqual(run("git", "add", "--", report_relative).returncode, 0)
                    result = run(sys.executable, "tools/check_repository.py")
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertIn("retired working-document path", result.stderr)
            report.rename(root / ".private/audit.md")
            readme.write_text("[Decisions](docs/adr/README.md)", encoding="utf-8")
            result = run(sys.executable, "tools/check_repository.py")
            self.assertEqual(result.returncode, 0, result.stderr)
            readme.write_text("[Decisions](docs/adr/README.md) [Private](.private/audit.md)", encoding="utf-8")
            result = run(sys.executable, "tools/check_repository.py")
            self.assertEqual(result.returncode, 1, result.stderr)
            self.assertIn("local link target is not publishable", result.stderr)

    def test_archive_preserves_executable_modes_on_every_build_host(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stage = root / "routevane-test"
            stage.mkdir()
            for name in ("routevane", "start-routevane.sh", "README.txt"):
                (stage / name).write_text("fixture", encoding="utf-8")
            output = root / "release.zip"
            pack(stage, output, "linux-amd64", "2026-09-01T00:00:00+00:00")
            with zipfile.ZipFile(output) as archive:
                for entry in archive.infolist():
                    self.assertEqual(entry.create_system, 3)
                    mode = entry.external_attr >> 16
                    self.assertTrue(stat.S_ISREG(mode))
                    expected = 0o644 if entry.filename.endswith("README.txt") else 0o755
                    self.assertEqual(stat.S_IMODE(mode), expected)


if __name__ == "__main__":
    unittest.main()
