"""New upstream findings cannot disappear behind repaired package identities."""

import contextlib
import io
import json
from pathlib import Path
import shutil
import tempfile
import unittest

import check_web_derivatives as checks


class AdvisoryMonitoringTests(unittest.TestCase):
    def problems(self, response):
        with contextlib.redirect_stdout(io.StringIO()):
            return checks.advisory_problems(response)

    def test_reviewed_repairs_and_withdrawn_advisory_pass(self):
        self.assertEqual(self.problems({name: [dict(repair)]
                                       for name, repair in checks.REPAIRS.items()}), [])
        self.assertEqual(self.problems({}), [])

    def test_new_finding_in_already_repaired_package_blocks(self):
        repair = dict(checks.REPAIRS["braces"])
        new = dict(repair, url="https://github.com/advisories/GHSA-new-finding", severity="critical")
        self.assertEqual(len(self.problems({"braces": [repair, new]})), 1)
        self.assertEqual(len(self.problems({"braces": [new]})), 1)

    def test_changed_existing_advisory_blocks(self):
        for key, value in (("vulnerable_versions", "*"), ("title", "broader defect"),
                           ("severity", "critical")):
            with self.subTest(key=key):
                changed = dict(checks.REPAIRS["braces"], **{key: value})
                self.assertEqual(len(self.problems({"braces": [changed]})), 1)

    def test_lower_severities_remain_visible(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.assertEqual(checks.advisory_problems({"braces": [{
                "url": "https://github.com/advisories/GHSA-low", "severity": "moderate"
            }]}), [])
        self.assertIn("moderate", output.getvalue())

    def test_invalid_registry_data_fails_closed(self):
        for response in ([], {"other": []}, {"braces": {}}, {"braces": [{}]}):
            with self.subTest(response=response), self.assertRaises(ValueError):
                self.problems(response)


class RepairInventoryTests(unittest.TestCase):
    def test_identity_and_override_are_enforced(self):
        with tempfile.TemporaryDirectory(dir=checks.ROOT / "tmp") as directory:
            root = Path(directory)
            (root / "web").mkdir()
            for name in ("package.json", "package-lock.json"):
                shutil.copyfile(checks.ROOT / "web" / name, root / "web" / name)
            checks.verify_inventory(root)
            lock_path = root / "web/package-lock.json"
            lock = json.loads(lock_path.read_text())
            original_lock = lock_path.read_bytes()
            braces = next(path for path in lock["packages"] if path.endswith("node_modules/braces"))
            lock["packages"][braces] = dict(lock["packages"][braces], integrity="sha512-unreviewed")
            lock_path.write_text(json.dumps(lock), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "unreviewed braces identity"):
                checks.verify_inventory(root)
            lock_path.write_bytes(original_lock)
            package = json.loads((root / "web/package.json").read_text())
            package["overrides"]["braces"] = "3.0.3"
            (root / "web/package.json").write_text(json.dumps(package), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "override does not match"):
                checks.verify_inventory(root)


if __name__ == "__main__":
    unittest.main()
