# Changelog

All notable user-facing changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use
[Semantic Versioning](https://semver.org/). Entries are generated from the
Conventional Commit history with [git-cliff](https://git-cliff.org) and then
curated by hand in the Vue-like layout defined by `cliff.toml` and checked by
the release policy in `relkit.toml`. Only `feat`, `fix`, `perf`, and
`revert` commits appear, and a breaking change always does.

This file starts at the first published version. The work before it built the
product and has no release to describe, so there are no historical entries to
reconstruct.

While Routevane is `0.y.z` nothing about its surface is guaranteed stable: a
breaking change raises the minor, everything else raises the patch. The
[release policy](docs/releasing.md) states which surfaces a version protects.

## [Unreleased]

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
