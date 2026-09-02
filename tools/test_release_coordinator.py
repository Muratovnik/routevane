"""Project-owned managed-release and native archive selection contracts."""

import re
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

import release_smoke


ROOT = Path(__file__).resolve().parents[1]


class ManagedReleaseContracts(unittest.TestCase):
    def setUp(self):
        self.settings = tomllib.loads((ROOT / "relkit.toml").read_text(encoding="utf-8"))["release"]

    def test_prepared_version_is_only_the_first_numbered_changelog_entry(self):
        pattern = re.compile(self.settings["version_pattern"], re.MULTILINE)
        changelog = (ROOT / self.settings["version_file"]).read_text(encoding="utf-8")
        self.assertEqual(1, pattern.groups)
        self.assertEqual(1, len(pattern.findall(changelog)))
        future = "## [Unreleased]\n\n## [1.2.3](https://example.invalid/compare/v1.2.2...v1.2.3)\n\n" + changelog
        self.assertEqual(["1.2.3"], pattern.findall(future))
        self.assertEqual([], pattern.findall("## [Unreleased]\n"))

    def test_asset_set_matches_every_shipped_platform_and_checksum_manifest(self):
        self.assertEqual(
            {f"routevane-v1.2.3-{target}.zip" for target in release_smoke.SUPPORTED} | {"SHA256SUMS"},
            {name.format(tag="v1.2.3", version="1.2.3") for name in self.settings["assets"]},
        )
        self.assertEqual("SHA256SUMS", self.settings["checksum_file"])
        self.assertTrue(self.settings["require_guard"])
        self.assertTrue(self.settings["owner_audit"])
        self.assertNotIn("branch", self.settings)
        self.assertEqual(["check", "test-browser"], [command[-1] for command in self.settings["checks"]])

    def test_native_selection_has_no_unknown_architecture_fallback(self):
        cases = [("Windows", "AMD64", "windows-amd64"), ("Windows", "ARM64", "windows-arm64"),
                 ("Linux", "x86_64", "linux-amd64"), ("Linux", "aarch64", "linux-arm64"),
                 ("Darwin", "arm64", "darwin-arm64")]
        for system, machine, expected in cases:
            with self.subTest(system=system, machine=machine), patch("release_smoke.platform.system", return_value=system), patch("release_smoke.platform.machine", return_value=machine):
                self.assertEqual(expected, release_smoke.native_target())
        for system, machine in (("Darwin", "x86_64"), ("Linux", "riscv64"), ("FreeBSD", "amd64")):
            with self.subTest(system=system, machine=machine), patch("release_smoke.platform.system", return_value=system), patch("release_smoke.platform.machine", return_value=machine):
                with self.assertRaisesRegex(ValueError, "no shipped native archive"):
                    release_smoke.native_target()

    def test_new_config_command_and_old_ci_command_call_same_smoke(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(prefix="managed release ", dir=scratch) as directory:
            assets = Path(directory)
            archive = assets / "routevane-v1.2.3-windows-amd64.zip"
            archive.write_bytes(b"test selection only")
            substitutions = {"python": "python", "assets": str(assets), "tag": "v1.2.3", "temp": str(assets / "scratch")}
            command = [part.format(**substitutions) for part in self.settings["smoke"][0]]
            self.assertEqual("tools/release_smoke.py", command[1])
            with patch("release_smoke.native_target", return_value="windows-amd64"), patch("release_smoke.smoke") as smoke:
                release_smoke.main(command[2:])
                smoke.assert_called_once_with(archive.resolve(), "v1.2.3", "windows-amd64", assets / "scratch")
            with patch("release_smoke.smoke") as smoke:
                release_smoke.main(["--archive", str(archive), "--version", "v1.2.3", "--platform", "windows-amd64"])
                smoke.assert_called_once_with(archive.resolve(), "v1.2.3", "windows-amd64", None)

    def test_download_selection_rejects_path_and_version_injection(self):
        for version in ("../v1.2.3", "v01.2.3", "v1.2.3-rc.1", "1.2.3"):
            with self.subTest(version=version), self.assertRaises(ValueError):
                release_smoke.archive_for(Path("assets"), version, "windows-amd64")

    def test_default_scratch_is_project_local_and_wrong_platform_cannot_extract(self):
        with patch("release_smoke.native_target", return_value="windows-amd64"), patch("release_smoke.tempfile.TemporaryDirectory", side_effect=RuntimeError("stop before extraction")) as temporary:
            with self.assertRaisesRegex(ValueError, "native smoke requested"):
                release_smoke.smoke(Path("missing.zip"), "v1.2.3", "linux-arm64")
            temporary.assert_not_called()
            with self.assertRaisesRegex(RuntimeError, "stop before extraction"):
                release_smoke.smoke(Path("missing.zip"), "v1.2.3", "windows-amd64")
            self.assertEqual(ROOT / "tmp", temporary.call_args.kwargs["dir"])


if __name__ == "__main__":
    unittest.main()
