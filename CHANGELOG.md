# Changelog

All notable user-facing changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use
[Semantic Versioning](https://semver.org/). Entries are generated from the
Conventional Commit history with [git-cliff](https://git-cliff.org) and then
curated by hand in the Vue-like layout defined by `cliff.toml` and checked by
the release policy in `relkit.toml`. Generated change bullets include only
`feat`, `fix`, `perf`, and `revert` commits, and a breaking change always appears.
Curated highlights may also summarize documentation and release-process updates.

This file starts at the first published version. The work before it built the
product and has no release to describe, so there are no historical entries to
reconstruct.

While Routevane is `0.y.z` nothing about its surface is guaranteed stable: a
breaking change raises the minor, everything else raises the patch. The
[release policy](docs/releasing.md) states which surfaces a version protects.

## [Unreleased]

## [0.1.3](https://github.com/Muratovnik/routevane/compare/v0.1.2...v0.1.3) (2026-09-04)

### Highlights

This release turns list overlaps into an automatic, route-local priority rule,
adds a reviewed configuration transfer, and makes the operational interface much
clearer while a list is loading, refreshing, failing, or being edited. It also
adds Russian user documentation and clearer Keenetic route ownership.

### Features

- **routes:** Resolve shared destinations by list priority, with drag-and-drop and keyboard reordering, while preserving unique address coverage. ([0d0b410](https://github.com/Muratovnik/routevane/commit/0d0b4101151f0143569e598a8954a80bbca8bf28))
- **config:** Export, preview, validate, and import portable Routevane configuration into a fresh installation. ([a7e542c](https://github.com/Muratovnik/routevane/commit/a7e542c4d5559af44abe089e6379739bb76167aa), [620f86b](https://github.com/Muratovnik/routevane/commit/620f86bf4abc372d9f0f7236e224f8a44c9bcbbc))
- **keenetic:** Label managed static routes with their category and list, and retain enough ownership data to update them safely. ([0c50dff](https://github.com/Muratovnik/routevane/commit/0c50dffcbb2f1249ca4160e0a1e9b5217fee54dd), [a7bbd4a](https://github.com/Muratovnik/routevane/commit/a7bbd4a6bc77fde8136ecc5cfb0a17256380144a))
- **ui:** Add the Routevane mark, adopt maintained Nuxt UI interaction mechanics, and add Russian user guides. ([7288827](https://github.com/Muratovnik/routevane/commit/72888276094a2c5f143476c0438e4daea40dc6a6), [a9e9282](https://github.com/Muratovnik/routevane/commit/a9e92827c4b567453c937b5a3a4d3a3337ffadb0), [d7f8448](https://github.com/Muratovnik/routevane/commit/d7f844830110263695943fc3912e36ab50e1c130))

### Bug Fixes

- **ui:** Keep loading, refresh, error, disabled, drawer, form, and dense-list states unambiguous and operable. ([eaa1c88](https://github.com/Muratovnik/routevane/commit/eaa1c88ba00c4bc8ad4a4e2f99cb09ba43cb70ab), [0d44270](https://github.com/Muratovnik/routevane/commit/0d44270af3614a476c7c5e9b7be55415645758ce))
- **config:** Preserve exact transfer bytes and reject malformed or inconsistent configuration before changing the destination. ([1451360](https://github.com/Muratovnik/routevane/commit/1451360e5f1f1a3a9ded22fe74c5008a03f0f57b), [647c68f](https://github.com/Muratovnik/routevane/commit/647c68f2fd94d1331caf647208803cf7fcb51063), [ca58a69](https://github.com/Muratovnik/routevane/commit/ca58a69469fb64bcf3719fad2d61b596e2549f8f))
- **delivery:** Serialize manual and scheduled writes to a device, and stop owned development servers when their supervisor exits. ([1d8360d](https://github.com/Muratovnik/routevane/commit/1d8360d803d519723b85b693a92926afe5901f65), [52c3a8a](https://github.com/Muratovnik/routevane/commit/52c3a8aaea01cd924c495816f79e757aaa3408da))

## [0.1.2](https://github.com/Muratovnik/routevane/compare/v0.1.1...v0.1.2) (2026-09-03)

### Highlights

This release makes list loading and error recovery clearer, explains rules shared
by multiple lists, and expands the built-in catalog. Maintainers also get a
hot-reload development workflow and the managed release process with relkit 0.12.1.

### Features

- **release:** Use a managed release workflow with reviewed publication plans,
  resumable receipts, and verification of the published artifacts.
  ([916ee32](https://github.com/Muratovnik/routevane/commit/916ee32de88ded45d73d86c01024bb44b29a6e11))
- **catalog:** Add a bounded Cursor domain list.
  ([2f557ea](https://github.com/Muratovnik/routevane/commit/2f557eabd4f5a6b9c70088c58c11c8093752ddf8))
- **catalog:** Add GitHub Copilot, Twitch, and Kinopub domain lists.
  ([74d2d37](https://github.com/Muratovnik/routevane/commit/74d2d37d744025e6a6bce1dbafb90e12a50cea6d))
- **forecast:** Explain cross-list rule overlaps before building a route.
  ([6922895](https://github.com/Muratovnik/routevane/commit/692289523dc493fe825e9d7cc2572beac817dacd))
- **dev:** Add hot reload for the frontend and backend during local development.
  ([d0a5ddb](https://github.com/Muratovnik/routevane/commit/d0a5ddb25c14e96f75d7a9e2d74b0fbc43b83f4c))

### Bug Fixes

- **docs:** Keep internal work records out of the public source tree.
  ([00afeeb](https://github.com/Muratovnik/routevane/commit/00afeeb41d8e519594c44d035ccf74d735e14df0))
- **tooling:** Apply repository restrictions at their intended publication boundaries.
  ([6cd88dd](https://github.com/Muratovnik/routevane/commit/6cd88dd3782fabb62cd9f0a1d160abaecdfadef3))
- **tooling:** Parse workflow action references as YAML when checking pinned actions.
  ([894844c](https://github.com/Muratovnik/routevane/commit/894844c827a5efededeeef844ca72e6db0a1f29e))
- **keenetic:** Reject ambiguous DNS route snapshots instead of accepting uncertain state.
  ([bab1010](https://github.com/Muratovnik/routevane/commit/bab10100ebfb787e87cd700ee93f2cc73b3e0ae4))
- **ui:** Keep partial list contents readable during loading, expose recoverable errors,
  and prevent unavailable actions from appearing ready to use.
  ([32803af](https://github.com/Muratovnik/routevane/commit/32803af9178e061c2f0cb84ae730b6951e7152bc))

## [0.1.1](https://github.com/Muratovnik/routevane/compare/v0.1.0...v0.1.1) (2026-09-01)

### Highlights

This maintenance release improves the installation documentation and release
process. Application behavior, the API, CLI, and archive layout are unchanged.

- Simplify the quick start, explain the included launcher, and separate download
  verification, updates, backups, and troubleshooting into a user maintenance guide.
  ([3190d5a](https://github.com/Muratovnik/routevane/commit/3190d5a46760808474e2fc1f1a125b6879fd221b))
- Validate the curated Vue-like changelog entry before both building and
  publishing a release, preserving the reviewed text and commit links.
  ([f1a8871](https://github.com/Muratovnik/routevane/commit/f1a887161509d5ec3477bb761910bcb3420e261f))

## [0.1.0](https://github.com/Muratovnik/routevane/releases/tag/v0.1.0) (2026-09-01)

### Highlights

Routevane's first public release turns a local route library into verified,
immutable files for routers and client applications. It ships as one binary
with an embedded browser UI, clear archive installation paths, explicit device
consent, and a release pipeline that verifies the same tagged source on every
supported native runner before publishing it.

### Breaking Changes

- Building Routevane from source requires Go 1.27. Release archives remain
  self-contained and do not require Go or Node.js on the user's machine.

### Features

- Compose reusable service and category libraries, observe maintained DNS and
  HTTPS sources, inspect forecasts, and publish immutable subscriptions.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Export six end-user formats for Keenetic, OpenWrt, MikroTik, Amnezia,
  sing-box, and Keenetic FQDN groups, plus Raw JSON diagnostics.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Extend refresh and rendering through contained external plugins with a
  versioned SDK and bounded permissions.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Bind an output to one registered device and opt into scheduled delivery
  through the same probe, backup, deploy, verify, and rollback lifecycle used
  by manual delivery.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Download verified release archives for Windows amd64/arm64, Linux
  amd64/arm64, and macOS arm64 with license, notices, SBOM, checksums, and
  launchers included.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed),
  [83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))

### Bug Fixes

- Separate user installation from contributor setup; include offline lifecycle
  instructions and open the browser only after the embedded UI is ready.
  ([83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))
- Preserve executable permissions in Unix archives produced on Windows and
  verify the actual shipped launchers with fresh runtime data.
  ([83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))
- Replace stale plugin templates with runnable, tested example files.
  ([83dd181](https://github.com/Muratovnik/routevane/commit/83dd181e1dfa9f9b59c386d2a3aaf2e12858b4e1))
- Build from source paths containing spaces without bypassing type checking;
  keep child-command diagnostics visible when a developer command fails.
  ([f99000d](https://github.com/Muratovnik/routevane/commit/f99000d56c0f6f7c225682e33bbd5c913c88b4d9))
- Keep Go 1.27 analysis and sandboxed Chromium acceptance working on Linux;
  verify browser-opening behavior in both embedded-UI and API-only builds.
  ([f2f7adc](https://github.com/Muratovnik/routevane/commit/f2f7adc5f253dc724899caff1dd765a170b01d4a),
  [4adbd60](https://github.com/Muratovnik/routevane/commit/4adbd607a81e57e80b9ebe8d0c56068dc524043b))
- Keep output builds and scheduled delivery attempts independent so one
  timeout or invalid output cannot starve its siblings or replace a previous
  valid artifact.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Revalidate output binding, connection metadata, consent, and credentials as
  one authorization decision before unattended device changes.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Refuse secret-bearing or non-local device destinations before they can be
  persisted, and revoke stored consent when connection metadata changes.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
- Contain verified plugin snapshots and remove stale combobox highlights from
  the browser control surface.
  ([c37bccb](https://github.com/Muratovnik/routevane/commit/c37bccb4462c3ddc56641df3fd3a70b639d7eeed))
