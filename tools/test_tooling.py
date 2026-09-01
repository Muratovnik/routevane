"""Behavioral regression tests for documentation and archive contracts."""

import stat
import tempfile
import unittest
import zipfile
from pathlib import Path

from check_repository import documentation_problems, scanner_problems, supported
from release_archive import pack


class RepositoryContracts(unittest.TestCase):
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

    def test_archive_preserves_executable_modes_on_every_build_host(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            stage = root / "routevane-test"
            stage.mkdir()
            for name in ("routing-agent", "start-routevane.sh", "README.txt"):
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
