"""Contracts of the Windows installer smoke that runs before a desktop release."""

import shutil
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import installer_smoke


ROOT = Path(__file__).resolve().parents[1]
UPDATE_CONFIG = "owner: Muratovnik\nrepo: routevane\nprovider: github\nreleaseType: release\n"


class InstallerSmokeContracts(unittest.TestCase):
    def setUp(self):
        scratch = ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        directory = tempfile.TemporaryDirectory(prefix="installer smoke ", dir=scratch)
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)

    def install_layout(self, install_dir, *, update_config=UPDATE_CONFIG, marker=True):
        resources = install_dir / "resources"
        resources.mkdir(parents=True, exist_ok=True)
        (resources / "routevane.exe").write_bytes(b"core")
        (install_dir / "Uninstall Routevane.exe").write_bytes(b"uninstaller")
        if marker:
            (resources / "desktop-installed").write_text("nsis", encoding="utf-8")
        if update_config is not None:
            (resources / "app-update.yml").write_text(update_config, encoding="utf-8")

    def test_the_destination_is_the_last_unquoted_argument(self):
        # NSIS reads /D= verbatim to the end of the command line; quoting it, as
        # list-to-command-line conversion would for a path with spaces, breaks it.
        installer = self.root / "Routevane-1.2.3-x64-setup.exe"
        destination = self.root / "run one" / "installed"
        command = installer_smoke.installer_command(installer, destination)
        self.assertEqual(command, f'"{installer}" /S /D={destination}')

    def test_the_installer_is_selected_by_a_stable_version(self):
        assets = self.root / "assets"
        self.assertEqual(installer_smoke.installer_for(assets, "v1.2.3"), assets / "Routevane-1.2.3-x64-setup.exe")
        for version in ("1.2.3", "v1.2", "v01.2.3", "v1.2.3-rc.1", "../v1.2.3"):
            with self.subTest(version=version), self.assertRaises(ValueError):
                installer_smoke.installer_for(assets, version)

    def test_update_configuration_is_read_as_plain_keys(self):
        path = self.root / "app-update.yml"
        path.write_text("# generated\nowner: 'Muratovnik'\nrepo: \"routevane\"\nprovider: github\n", encoding="utf-8")
        self.assertEqual(installer_smoke.read_update_config(path),
                         {"owner": "Muratovnik", "repo": "routevane", "provider": "github"})

    def test_a_complete_installation_has_no_problems(self):
        install_dir = self.root / "installed"
        self.install_layout(install_dir)
        with patch("installer_smoke.reported_version", return_value="routevane v1.2.3"):
            self.assertEqual(installer_smoke.installation_problems(install_dir, "v1.2.3"), [])

    def test_each_mismatch_is_named(self):
        cases = {
            "wrong core version": ({}, "routevane v1.2.2", "routevane v1.2.2"),
            "missing installer marker": ({"marker": False}, "routevane v1.2.3", "desktop-installed"),
            "missing update configuration": ({"update_config": None}, "routevane v1.2.3", "app-update.yml"),
            "foreign update owner": ({"update_config": UPDATE_CONFIG.replace("Muratovnik", "someone")},
                                     "routevane v1.2.3", "owner"),
            "generic update provider": ({"update_config": UPDATE_CONFIG.replace("github", "generic")},
                                        "routevane v1.2.3", "provider"),
            "renamed update repository": ({"update_config": UPDATE_CONFIG.replace("repo: routevane", "repo: other")},
                                          "routevane v1.2.3", "repo"),
        }
        for name, (layout, reported, expected) in cases.items():
            with self.subTest(name):
                install_dir = self.root / name
                self.install_layout(install_dir, **layout)
                with patch("installer_smoke.reported_version", return_value=reported):
                    problems = installer_smoke.installation_problems(install_dir, "v1.2.3")
                self.assertEqual(len(problems), 1, problems)
                self.assertIn(expected, problems[0])

    def test_a_failed_check_still_uninstalls_and_removes_the_run_directory(self):
        installer = self.root / "Routevane-1.2.3-x64-setup.exe"
        installer.write_bytes(b"installer")
        scratch = self.root / "scratch"
        uninstalled = []

        def install(_installer, install_dir):
            self.install_layout(install_dir)

        def uninstall(uninstaller):
            uninstalled.append(uninstaller.name)
            shutil.rmtree(uninstaller.parent)

        with (patch("installer_smoke.require_windows_x64"),
              patch("installer_smoke.installed_products", return_value=[]),
              patch("installer_smoke.run_installer", side_effect=install),
              patch("installer_smoke.run_uninstaller", side_effect=uninstall),
              patch("installer_smoke.reported_version", return_value="routevane v0.0.1")):
            with self.assertRaisesRegex(ValueError, "routevane v0.0.1"):
                installer_smoke.smoke(installer, "v1.2.3", scratch)
        self.assertEqual(uninstalled, ["Uninstall Routevane.exe"])
        self.assertEqual(list(scratch.iterdir()), [])

    def test_an_uninstaller_that_leaves_the_core_behind_fails(self):
        installer = self.root / "Routevane-1.2.3-x64-setup.exe"
        installer.write_bytes(b"installer")
        with (patch("installer_smoke.require_windows_x64"),
              patch("installer_smoke.installed_products", return_value=[]),
              patch("installer_smoke.run_installer", side_effect=lambda _i, d: self.install_layout(d)),
              patch("installer_smoke.run_uninstaller"),
              patch("installer_smoke.reported_version", return_value="routevane v1.2.3"),
              patch("installer_smoke.UNINSTALL_DEADLINE_SECONDS", 0.2)):
            with self.assertRaisesRegex(ValueError, "uninstall"):
                installer_smoke.smoke(installer, "v1.2.3", self.root / "scratch")

    def test_an_existing_installation_is_never_replaced(self):
        # The installer carries the production identity: on a workstation it
        # would take over the operator's own Routevane and uninstall it after.
        installer = self.root / "Routevane-1.2.3-x64-setup.exe"
        installer.write_bytes(b"installer")
        with (patch("installer_smoke.require_windows_x64"),
              patch("installer_smoke.installed_products", return_value=["Routevane 0.3.2"]),
              patch("installer_smoke.run_installer") as install):
            with self.assertRaisesRegex(ValueError, "already installed"):
                installer_smoke.smoke(installer, "v1.2.3", self.root / "scratch")
        install.assert_not_called()
        self.assertFalse((self.root / "scratch").exists())

    def test_release_installs_the_built_installer_before_publishing_it(self):
        workflow = (ROOT / ".github/workflows/release.yml").read_text(encoding="utf-8")
        desktop = workflow.split("\n  desktop:\n", 1)[1].split("\n  candidate:\n", 1)[0]
        build = desktop.index('./tools/dev.ps1 installer -Version "$env:VERSION"')
        smoke = desktop.index('python tools/installer_smoke.py --assets .cache/desktop-release --version "$env:VERSION"')
        upload = desktop.index("actions/upload-artifact@")
        self.assertLess(build, smoke)
        self.assertLess(smoke, upload)
        self.assertIn("if ($LASTEXITCODE -ne 0) { throw 'installer acceptance failed' }", desktop[smoke:upload])


if __name__ == "__main__":
    unittest.main()
