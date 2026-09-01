"""Exercise the shipped notes command with Routevane's real opt-in policy."""

import re
import shutil
import subprocess
import sys
import tempfile
import tomllib
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
LINK = "[abcdef1](https://example.invalid/commit/abcdef1234567890)"
ENTRY = (
    "## [0.1.0](https://example.invalid/releases/tag/v0.1.0) (2026-09-01)\n\n"
    "### Highlights\n\nA curated résumé, not a regenerated commit dump.\n\n"
    "### BREAKING CHANGES\n\n#### Migration\n\nKeep an offline backup.\n\n"
    f"### Features\n\n- A grouped change without a scope or PR. ({LINK})\n"
)


class ReleaseNotesContracts(unittest.TestCase):
    def setUp(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        directory = tempfile.TemporaryDirectory(prefix="release notes ", dir=scratch)
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        shutil.copyfile(ROOT / "relkit.toml", self.root / "relkit.toml")
        self.output = self.root / "release notes.md"

    def notes(self, source, version="v0.1.0", output="release notes.md"):
        (self.root / "CHANGELOG.md").write_text(source, encoding="utf-8", newline="")
        return subprocess.run(
            [
                sys.executable, str(ROOT / ".github/relkit.pyz"), "notes", version,
                "--root", str(self.root), "--output", output,
            ],
            cwd=ROOT,
            capture_output=True,
            text=True,
            encoding="utf-8",
            timeout=30,
        )

    def test_real_policy_preserves_curated_first_release_and_ignores_unreleased(self):
        source = "# Changelog\n\n## [Unreleased]\n\n" + ENTRY + "\n\n"
        result = self.notes(source)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.output.read_bytes(), ENTRY.encode("utf-8"))

    def test_next_release_requires_and_accepts_a_comparison(self):
        entry = ENTRY.replace("[0.1.0]", "[0.1.1]").replace(
            "/releases/tag/v0.1.0", "/compare/v0.1.0...v0.1.1"
        )
        result = self.notes(entry, "v0.1.1")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.output.read_bytes(), entry.encode("utf-8"))
        result = self.notes(
            entry.replace("/compare/v0.1.0...v0.1.1", "/releases/tag/v0.1.1"),
            "v0.1.1",
        )
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertEqual(self.output.read_bytes(), entry.encode("utf-8"))

    def test_invalid_notes_cannot_replace_or_create_an_export(self):
        cases = {
            "missing version": "## [Unreleased]\n",
            "empty entry": "## [0.1.0]\n",
            "duplicate entry": ENTRY + "\n" + ENTRY,
            "missing traceability": ENTRY.replace(f" ({LINK})", ""),
            "wrong hash": ENTRY.replace("[abcdef1]", "[1234567]"),
            "invalid date": ENTRY.replace("2026-09-01", "2026-02-30"),
            "unknown section": ENTRY.replace("### Features", "### Other"),
            "empty section": ENTRY + "\n### Reverts\n",
        }
        for name, source in cases.items():
            for existing in (False, True):
                with self.subTest(case=name, existing=existing):
                    self.output.unlink(missing_ok=True)
                    if existing:
                        self.output.write_bytes(b"previous validated notes\n")
                    result = self.notes(source)
                    self.assertEqual(result.returncode, 1, result.stderr)
                    self.assertRegex(result.stderr, r"CHANGELOG\.md:\d+:")
                    if existing:
                        self.assertEqual(self.output.read_bytes(), b"previous validated notes\n")
                    else:
                        self.assertFalse(self.output.exists())

    def test_invalid_policy_is_not_silently_downgraded(self):
        policy = self.root / "relkit.toml"
        policy.write_text('[changelog]\nprofile = "typo"\n', encoding="utf-8")
        result = self.notes(ENTRY)
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertIn("relkit.toml", result.stderr)
        self.assertFalse(self.output.exists())

    def test_export_cannot_overwrite_its_changelog(self):
        result = self.notes(ENTRY, output="CHANGELOG.md")
        self.assertEqual(result.returncode, 2, result.stderr)
        self.assertEqual((self.root / "CHANGELOG.md").read_bytes(), ENTRY.encode("utf-8"))

    def test_workflow_checks_notes_before_build_and_publication(self):
        # This is the exact command contract used in the two publication boundaries,
        # not a second implementation of release-kit's Markdown parser.
        workflow = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8")
        block = (
            'python tools/release_gate.py --version "$env:VERSION" '
            '--expected-sha "$env:EXPECTED_SHA"\n'
            "          if ($LASTEXITCODE -ne 0) { throw 'release identity check failed' }\n"
            '          python .github/relkit.pyz notes "$env:VERSION" --output '
            "(Join-Path $env:RUNNER_TEMP 'release-notes.md')\n"
            "          if ($LASTEXITCODE -ne 0) { throw 'curated release notes check failed' }"
        )
        positions = [match.start() for match in re.finditer(re.escape(block), workflow)]
        self.assertEqual(len(positions), 2)
        self.assertLess(positions[0], workflow.index("./tools/dev.ps1 release"))
        self.assertLess(workflow.index("  publish:"), positions[1])
        self.assertLess(positions[1], workflow.index("gh release create"))

    def test_template_uses_a_supported_breaking_field_parser(self):
        config = tomllib.loads((ROOT / "cliff.toml").read_text(encoding="utf-8"))["git"]
        self.assertTrue(config["protect_breaking_commits"])
        parsers = config["commit_parsers"]
        matching = [
            (index, parser) for index, parser in enumerate(parsers)
            if parser.get("field") == "breaking"
        ]
        self.assertEqual(len(matching), 1)
        index, parser = matching[0]
        self.assertRegex("true", parser["pattern"])
        self.assertIn("BREAKING CHANGES", parser["group"])
        ordinary_index = next(i for i, rule in enumerate(parsers) if rule.get("message") == "^feat")
        self.assertLess(index, ordinary_index)


if __name__ == "__main__":
    unittest.main()
