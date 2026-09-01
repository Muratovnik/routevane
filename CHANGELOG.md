# Changelog

All notable user-facing changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use
[Semantic Versioning](https://semver.org/). Entries are generated from the
Conventional Commit history with [git-cliff](https://git-cliff.org) and then
curated by hand; `cliff.toml` owns the layout. Only `feat`, `fix`, `perf`, and
`revert` commits appear, and a breaking change always does.

This file starts at the first published version. The work before it built the
product and has no release to describe, so there are no historical entries to
reconstruct.

While Routevane is `0.y.z` nothing about its surface is guaranteed stable: a
breaking change raises the minor, everything else raises the patch. `README.md`
states what a version protects once the first one is published.

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
- Export six end-user formats for Keenetic, OpenWrt, MikroTik, Amnezia,
  sing-box, and Keenetic FQDN groups, plus Raw JSON diagnostics.
- Extend refresh and rendering through contained external plugins with a
  versioned SDK and bounded permissions.
- Bind an output to one registered device and opt into scheduled delivery
  through the same probe, backup, deploy, verify, and rollback lifecycle used
  by manual delivery.
- Download verified release archives for Windows amd64/arm64, Linux
  amd64/arm64, and macOS arm64 with license, notices, SBOM, checksums, and
  launchers included.

### Bug Fixes

- Keep output builds and scheduled delivery attempts independent so one
  timeout or invalid output cannot starve its siblings or replace a previous
  valid artifact.
- Revalidate output binding, connection metadata, consent, and credentials as
  one authorization decision before unattended device changes.
- Refuse secret-bearing or non-local device destinations before they can be
  persisted, and revoke stored consent when connection metadata changes.
- Contain verified plugin snapshots and remove stale combobox highlights from
  the browser control surface.
