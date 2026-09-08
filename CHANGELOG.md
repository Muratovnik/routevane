# Changelog

All notable user-facing changes are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and releases use
[Semantic Versioning](https://semver.org/). While Routevane is `0.y.z`, a
breaking change raises the minor version and everything else raises the patch.
The [release policy](docs/releasing.md) states which surfaces a version protects
and how entries are generated and validated.

This file starts at the first published version, 0.1.0.

## [Unreleased]

### Changed

- Keenetic DNS builds use output-scoped, collision-checked group names and an
  optional per-output prefix. Delivery changes only groups with persisted exact
  ownership. Existing files remain immutable; unowned name conflicts require
  manual review and migration, including after loss of the ownership ledger.

### Fixed

- Keenetic save errors inside HTTP 200 responses now fail delivery. Recovery must
  save and read back the restored configuration; recovery failure takes priority
  over the original verification error.
- Keenetic DNS delivery detects and repairs `auto` policy drift on owned groups.
- Browser discovery blocks direct WebRTC UDP and enforces the host limit while
  connections are still pending.
- Scheduled refresh preserves newer schedule choices and includes profiles beyond
  the first 200. Build capacity is checked after overlapping rules are pruned.
- The web interface restores send-menu navigation, visible retry states and
  catalog refresh after busy operations. Tabs preserve keyboard access and drafts;
  repeated single selections close their picker, including when it becomes busy.
- Plugin shutdown remains bounded when a plugin stops reading its input, and
  Windows executable cleanup tolerates transient locks after process exit.

## [0.1.7](https://github.com/Muratovnik/routevane/compare/v0.1.5...v0.1.7) (2026-09-06)

### Highlights

This release adds a Windows desktop application. Routevane opens in its own
window, keeps scheduled work running from the tray, and an installed build
offers a newer version that one explicit click downloads, installs and
restarts. The operational interface is rebuilt around ordered tables for routes
and lists, with category filtering, steadier selection and clearer loading and
refresh feedback. The CLI package stays independently distributable and does
not require the desktop application.

### Features

- **desktop:** Open Routevane in its own application window, with a tray lifetime that keeps owned background work running after the window is closed. ([59f0a82](https://github.com/Muratovnik/routevane/commit/59f0a8285c6762f605df23ce55cad8398fd24063))
- **desktop:** Install Windows builds from a per-user installer, and update an installed build from GitHub releases on one explicit click; a failed download leaves the current version usable. ([49a4778](https://github.com/Muratovnik/routevane/commit/49a4778740c56946cc21dd1ae52ab2d847d9b9a9))
- **ui:** Rebuild route composition and the library as ordered tables with category filtering and shared desktop workspaces. ([1689947](https://github.com/Muratovnik/routevane/commit/168994745ad75eee052e6e1591de7e1b3320c2cf), [744555f](https://github.com/Muratovnik/routevane/commit/744555ff6b8511805a33ad50648d76c263e343c4), [01e5027](https://github.com/Muratovnik/routevane/commit/01e5027c80b6e1a329c357c1dc7d4a911ddfb9c9))
- **priority:** Give a new route its library default list order and summarize cross-list overlaps before building. ([4dfb142](https://github.com/Muratovnik/routevane/commit/4dfb142e39996321bf669bfee5433cebbc1be76c))

### Bug Fixes

- **ui:** Keep route and list selection stable in one table while inspecting, filtering and reordering. ([aff5ddf](https://github.com/Muratovnik/routevane/commit/aff5ddfec9a2f147f40c659f9eadb64bcd9f31e7), [723dc2b](https://github.com/Muratovnik/routevane/commit/723dc2bc7f94c422952409aca067bb66cf7743ea), [370164e](https://github.com/Muratovnik/routevane/commit/370164e82ca95e01555b507a1ac4e05a09105273), [86522e2](https://github.com/Muratovnik/routevane/commit/86522e261bb34ad1f41d894e0a6467cebdc051e7))
- **ui:** Give each composition workspace one scroll owner, and keep docked panels from shifting the page around them. ([f960fee](https://github.com/Muratovnik/routevane/commit/f960fee1d531cfa5382646b6513e454ed012c0b3), [aab825a](https://github.com/Muratovnik/routevane/commit/aab825a1e74368d11a23df5cb8c9827ca775a62a), [5c98618](https://github.com/Muratovnik/routevane/commit/5c986188b5256708056cb573c3040d6003c68a41))
- **ui:** Report loading, refreshing and inspection consistently across pages instead of flashing transient states, and keep partial forecasts visible. ([f402ce1](https://github.com/Muratovnik/routevane/commit/f402ce17c9b23b6a56aac215d96c2d3bc69e6a83), [26e0435](https://github.com/Muratovnik/routevane/commit/26e04351a47f3d5f827a6bd9fe438362e0cde9c5), [e1756c4](https://github.com/Muratovnik/routevane/commit/e1756c4d9d1e969279a37c06b2ff0603c21fbaf5), [61fc855](https://github.com/Muratovnik/routevane/commit/61fc855acccf7b90d8981a9a88198f8cec2a6c5e))
- **priority:** Remove the global cap on the size of a route's list order. ([92eff42](https://github.com/Muratovnik/routevane/commit/92eff4221d3a1f0d6b5982307c2b2f765a2688ea))
- **web:** Debounce list card toggles so rapid changes are not lost. ([c955e65](https://github.com/Muratovnik/routevane/commit/c955e6505a5e1671a05d0c3b1cbb1fab19cc3ed6))
- **release:** Publish the Windows installer, its blockmap and the updater feed under the release's single checksum manifest. ([e11f9ac](https://github.com/Muratovnik/routevane/commit/e11f9ac0208d0680939604314f2196532d10890a))

## [0.1.5](https://github.com/Muratovnik/routevane/compare/v0.1.3...v0.1.5) (2026-09-04)

### Highlights

This release contains the Routevane 0.1.3 product changes and corrects the
release validation deadline exposed by Linux race instrumentation.

### Bug Fixes

- **release:** Allow instrumented server startup to reach its listener without weakening the readiness assertion, and report an early process exit directly. ([a7a63c4](https://github.com/Muratovnik/routevane/commit/a7a63c402324c383858597b35459b4e5ff48db33))
- **release:** Await the source-refresh response instead of racing drawer focus during browser validation. ([64a06f6](https://github.com/Muratovnik/routevane/commit/64a06f6d0d32aefb14e03f3122c8c0f52a028aeb))

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
