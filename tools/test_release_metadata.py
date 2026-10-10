"""Release notices preserve a verified missing upstream license without a blanket fallback."""

import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import release_metadata as metadata


ZIGPTY_LICENSE = """MIT License

Copyright (c) Pooya Parsa <pooya@pi0.io>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
"""


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


    def test_exact_zigpty_tarball_and_shipped_notice_are_both_required(self):
        scratch = metadata.ROOT / "tmp"
        scratch.mkdir(exist_ok=True)
        with tempfile.TemporaryDirectory(dir=scratch) as temporary:
            root = Path(temporary)
            web = root / "web"
            package = web / "node_modules" / "zigpty"
            package.mkdir(parents=True)
            (package / "package.json").write_text(
                json.dumps({"name": "zigpty", "version": "0.2.1"}), encoding="utf-8"
            )
            notice = package / "LICENSE"
            notice.write_bytes(ZIGPTY_LICENSE.encode("utf-8"))
            entry = {
                "version": "0.2.1",
                "integrity": "sha512-MR9JqJx2wf5f4wz8zpx050AlqrmWeIW+1h0SO5iEyhG3HFRjY5luC3szS2ux2EGuPjE5OU9ZAuiCBeMWBTrqZw==",
            }
            lock = {"packages": {"node_modules/zigpty": entry}}

            def write():
                (web / "package-lock.json").write_text(json.dumps(lock), encoding="utf-8")

            with patch.object(metadata, "ROOT", root):
                write()
                components = metadata.npm_components()
                self.assertEqual(len(components), 1)
                self.assertEqual(components[0].declared_license, "MIT")
                self.assertEqual(components[0].license_texts, (("LICENSE", ZIGPTY_LICENSE.strip()),))
                self.assertIn(ZIGPTY_LICENSE.strip(), metadata.notices("v1.0.0", components))
                for field, value in (("version", "0.2.2"), ("integrity", "sha512-other")):
                    with self.subTest(field=field):
                        previous = entry[field]
                        entry[field] = value
                        write()
                        with self.assertRaisesRegex(ValueError, "incomplete license identity"):
                            metadata.npm_components()
                        entry[field] = previous
                write()
                (package / "package.json").write_text(
                    json.dumps({"name": "unreviewed", "version": "0.2.1"}), encoding="utf-8"
                )
                with self.assertRaisesRegex(ValueError, "incomplete license identity"):
                    metadata.npm_components()
                (package / "package.json").write_text(
                    json.dumps({"name": "zigpty", "version": "0.2.1"}), encoding="utf-8"
                )
                for payload in (b"", b"MIT License\n", ZIGPTY_LICENSE.replace("Pooya Parsa", "Someone else").encode("utf-8")):
                    with self.subTest(license_bytes=len(payload)):
                        notice.write_bytes(payload)
                        with self.assertRaisesRegex(ValueError, "incomplete license identity"):
                            metadata.npm_components()
                notice.unlink()
                with self.assertRaisesRegex(ValueError, "incomplete license identity"):
                    metadata.npm_components()


if __name__ == "__main__":
    unittest.main()
