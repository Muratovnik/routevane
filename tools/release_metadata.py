#!/usr/bin/env python3
"""Generate deterministic dependency notices and an SPDX 2.3 release SBOM."""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import subprocess
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable
from urllib.parse import quote


ROOT = Path(__file__).resolve().parents[1]
LICENSE_NAME = re.compile(r"^(?:licen[cs]e|copying|notice)(?:$|[._-])", re.I)


@dataclass(frozen=True)
class Component:
    ecosystem: str
    name: str
    version: str
    declared_license: str
    license_texts: tuple[tuple[str, str], ...]

    @property
    def key(self) -> str:
        return f"{self.ecosystem}:{self.name}@{self.version}"

    @property
    def spdx_id(self) -> str:
        digest = hashlib.sha256(self.key.encode("utf-8")).hexdigest()[:20]
        return f"SPDXRef-Package-{digest}"

    @property
    def purl(self) -> str:
        kind = "golang" if self.ecosystem == "go" else "npm"
        return f"pkg:{kind}/{quote(self.name, safe='/')}@{quote(self.version, safe='')}"


def run(command: list[str]) -> str:
    result = subprocess.run(
        command,
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return result.stdout


def json_stream(text: str) -> Iterable[dict[str, Any]]:
    decoder = json.JSONDecoder()
    offset = 0
    while offset < len(text):
        while offset < len(text) and text[offset].isspace():
            offset += 1
        if offset == len(text):
            return
        value, offset = decoder.raw_decode(text, offset)
        if not isinstance(value, dict):
            raise ValueError("expected an object in the Go package stream")
        yield value


def license_files(directory: Path) -> tuple[tuple[str, str], ...]:
    if not directory.is_dir():
        return ()
    found: list[tuple[str, str]] = []
    for path in sorted(directory.iterdir(), key=lambda item: item.name.casefold()):
        if path.is_file() and LICENSE_NAME.match(path.name):
            found.append(
                (
                    path.name,
                    path.read_text(encoding="utf-8", errors="replace").strip(),
                )
            )
    return tuple(found)


def go_components() -> list[Component]:
    modules: dict[tuple[str, str], dict[str, Any]] = {}
    for package in json_stream(run(["go", "list", "-deps", "-json", "./cmd/routing-agent"])):
        module = package.get("Module")
        if not isinstance(module, dict) or module.get("Main"):
            continue
        name = str(module.get("Path", "")).strip()
        version = str(module.get("Version", "")).strip()
        effective = module.get("Replace") if isinstance(module.get("Replace"), dict) else module
        directory = Path(str(effective.get("Dir", "")))
        if not name or not version:
            raise ValueError(f"Go dependency has no stable identity: {module!r}")
        modules[(name, version)] = {"directory": directory}

    components: list[Component] = []
    for (name, version), metadata in sorted(modules.items()):
        texts = license_files(metadata["directory"])
        if not texts:
            raise ValueError(f"Go module carries no root license text: {name}@{version}")
        components.append(Component("go", name, version, "NOASSERTION", texts))
    return components


def declared_license(metadata: dict[str, Any]) -> str:
    value = metadata.get("license")
    if isinstance(value, str) and value.strip():
        return value.strip()
    values = metadata.get("licenses")
    if isinstance(values, list):
        declared = []
        for item in values:
            if isinstance(item, str) and item.strip():
                declared.append(item.strip())
            elif isinstance(item, dict) and isinstance(item.get("type"), str):
                declared.append(item["type"].strip())
        if declared:
            return " OR ".join(sorted(set(declared)))
    return ""


def npm_name(lock_path: str, installed: dict[str, Any]) -> str:
    value = installed.get("name")
    if isinstance(value, str) and value.strip():
        return value.strip()
    return lock_path.rsplit("node_modules/", 1)[-1]


def npm_components() -> list[Component]:
    lock = json.loads((ROOT / "web" / "package-lock.json").read_text(encoding="utf-8"))
    packages = lock.get("packages")
    if not isinstance(packages, dict):
        raise ValueError("web/package-lock.json carries no packages map")

    components: dict[tuple[str, str], Component] = {}
    for lock_path, metadata in sorted(packages.items()):
        if not lock_path or not isinstance(metadata, dict) or metadata.get("dev") is True:
            continue
        version = str(metadata.get("version", "")).strip()
        directory = ROOT / "web" / Path(lock_path)
        installed: dict[str, Any] = {}
        package_json = directory / "package.json"
        if package_json.is_file():
            installed = json.loads(package_json.read_text(encoding="utf-8"))
        name = npm_name(lock_path, installed)
        license_id = declared_license(metadata) or declared_license(installed)
        if not name or not version or not license_id:
            raise ValueError(f"npm production dependency has incomplete license identity: {lock_path}")
        key = (name, version)
        candidate = Component("npm", name, version, license_id, license_files(directory))
        previous = components.get(key)
        if previous is None or (not previous.license_texts and candidate.license_texts):
            components[key] = candidate
    return [components[key] for key in sorted(components)]


def spdx_license(value: str) -> str:
    if value == "NOASSERTION":
        return value
    if re.fullmatch(r"[A-Za-z0-9.+()\- ]+(?:AND|OR|WITH)?[A-Za-z0-9.+()\- ]*", value):
        return value
    return "NOASSERTION"


def created_at(value: str) -> str:
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    return parsed.astimezone(timezone.utc).replace(microsecond=0).strftime("%Y-%m-%dT%H:%M:%SZ")


def notices(version: str, components: list[Component]) -> str:
    lines = [
        f"Routevane {version} third-party notices",
        "",
        "The Routevane license is distributed separately as LICENSE.",
        "This inventory covers Go modules linked into routing-agent and production",
        "npm packages used to generate the embedded control surface.",
        "",
    ]
    for component in components:
        lines.extend(
            [
                "=" * 78,
                f"{component.ecosystem}: {component.name}@{component.version}",
                f"Declared license: {component.declared_license}",
            ]
        )
        if not component.license_texts:
            lines.append("Upstream package contains no root license text; declaration retained above.")
            lines.append("")
            continue
        for filename, text in component.license_texts:
            lines.extend([f"--- {filename} ---", text, ""])
    return "\n".join(lines).rstrip() + "\n"


def sbom(version: str, created: str, revision: str, components: list[Component]) -> dict[str, Any]:
    root_id = "SPDXRef-Package-Routevane"
    packages: list[dict[str, Any]] = [
        {
            "name": "Routevane",
            "SPDXID": root_id,
            "versionInfo": version,
            "downloadLocation": "NOASSERTION",
            "filesAnalyzed": False,
            "licenseConcluded": "MIT",
            "licenseDeclared": "MIT",
            "copyrightText": "NOASSERTION",
            "externalRefs": [
                {
                    "referenceCategory": "PACKAGE-MANAGER",
                    "referenceType": "purl",
                    "referenceLocator": f"pkg:golang/github.com/Muratovnik/routevane@{quote(version, safe='')}",
                }
            ],
        }
    ]
    relationships = [
        {
            "spdxElementId": "SPDXRef-DOCUMENT",
            "relationshipType": "DESCRIBES",
            "relatedSpdxElement": root_id,
        }
    ]
    for component in components:
        packages.append(
            {
                "name": component.name,
                "SPDXID": component.spdx_id,
                "versionInfo": component.version,
                "downloadLocation": "NOASSERTION",
                "filesAnalyzed": False,
                "licenseConcluded": "NOASSERTION",
                "licenseDeclared": spdx_license(component.declared_license),
                "copyrightText": "NOASSERTION",
                "externalRefs": [
                    {
                        "referenceCategory": "PACKAGE-MANAGER",
                        "referenceType": "purl",
                        "referenceLocator": component.purl,
                    }
                ],
            }
        )
        relationships.append(
            {
                "spdxElementId": root_id,
                "relationshipType": "DEPENDS_ON",
                "relatedSpdxElement": component.spdx_id,
            }
        )
    return {
        "spdxVersion": "SPDX-2.3",
        "dataLicense": "CC0-1.0",
        "SPDXID": "SPDXRef-DOCUMENT",
        "name": f"routevane-{version}",
        "documentNamespace": f"https://github.com/Muratovnik/routevane/sbom/{quote(version, safe='')}/{revision}",
        "creationInfo": {
            "created": created_at(created),
            "creators": ["Tool: Routevane-release-metadata-1"],
        },
        "documentDescribes": [root_id],
        "packages": packages,
        "relationships": relationships,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True)
    parser.add_argument("--created", required=True)
    parser.add_argument("--notices", required=True, type=Path)
    parser.add_argument("--sbom", required=True, type=Path)
    args = parser.parse_args()

    revision = run(["git", "rev-parse", "HEAD"]).strip()
    components = sorted(go_components() + npm_components(), key=lambda item: item.key)
    if not components:
        raise ValueError("release dependency inventory is empty")

    args.notices.parent.mkdir(parents=True, exist_ok=True)
    args.sbom.parent.mkdir(parents=True, exist_ok=True)
    args.notices.write_text(notices(args.version, components), encoding="utf-8", newline="\n")
    args.sbom.write_text(
        json.dumps(sbom(args.version, args.created, revision, components), ensure_ascii=False, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
        newline="\n",
    )
    print(f"release metadata: {len(components)} dependencies")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
