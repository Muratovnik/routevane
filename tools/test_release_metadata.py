"""Release notices preserve a verified missing upstream license without a blanket fallback."""

import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import release_metadata as metadata


class MissingNpmLicense(unittest.TestCase):
    def test_exact_tarball_retains_notice_and_unknown_identity_still_fails(self):
        scratch = metadata.ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=scratch) as temporary:
            root = Path(temporary)
            web = root / "web"
            web.mkdir()
            entry = {
                "version": "0.4.1",
                "integrity": "sha512-A6jOWOZX5yvyo1qMn7IveoWN91mJI5L3BUKsIwkg6qrTGgHs1Sb1JF/vyLJgnbN1rH4OOOxFbtqL9A46bOyGUQ==",
            }
            lock = {"packages": {"node_modules/vaul-vue": entry}}
            def write():
                (web / "package-lock.json").write_text(json.dumps(lock), encoding="utf-8")
            with patch.object(metadata, "ROOT", root):
                write()
                components = metadata.npm_components()
                self.assertEqual(components[0].declared_license, "MIT")
                self.assertIn("Copyright (c) 2025 unovue", metadata.notices("v1.0.0", components))
                for field, value in (("version", "0.4.2"), ("integrity", "sha512-other")):
                    previous = entry[field]
                    entry[field] = value
                    write()
                    with self.assertRaisesRegex(ValueError, "incomplete license identity"):
                        metadata.npm_components()
                    entry[field] = previous


if __name__ == "__main__":
    unittest.main()
