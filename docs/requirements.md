---
status: adopted
---

# Current product requirements

Audience: contributors deciding what Routevane must preserve. Installation belongs
in the [user guide](../README.md); implementation belongs in
[architecture](ARCHITECTURE.md) and the [UI contract](UI.md).
This document defines the current product contract and separates it from open
work. Technical rationale belongs in the [decisions](adr/README.md).

## Product contract

- Deliver a local Electron desktop application with its Chromium runtime and
  the existing Go core. Retain the independently runnable Go CLI and its embedded
  English/Russian UI/API. No cloud account or external database is required.
- Closing the desktop window hides it to a visible tray icon. Open restores the
  same window and draft; Quit stops its owned Go process and releases its data
  lock. A second launch activates the existing desktop instance. A shell crash
  ends the owned backend's lifetime. Independently started CLI processes are
  never adopted or terminated implicitly.
- Source refresh schedules belong to Go and keep running while the desktop is
  in the tray. Application updates belong to packaging, independently of those
  schedules; unattended application replacement is not enabled by this slice.
- The primary interface is a desktop window operated by keyboard and pointer.
  Preserve access in smaller windows and with enlarged content; this does not
  require a separate mobile interface. The [UI contract](UI.md#desktop-layout-and-window-adaptation)
  owns layout and accessibility verification.
- A **list** contains destinations; a **category** groups lists; a **profile**
  composes them and publishes through one or more **connections**. In the API,
  `services` still means UI lists and `lists` means UI profiles.
- Prefer safe domain-capable rules when the target supports them. DNS evidence
  never justifies WHOIS/RDAP/ASN expansion. Observation validity and policy
  acceptance are separate; decisions are deterministic and reason-coded.
- Keep published plans and files immutable, validate before publication, and
  preserve the previous verified file when rebuilding fails.
- Separate global library editing from per-profile selection. Permit library
  removal only when stored profiles are not broken. Profiles are archived, not
  destructively deleted; published history remains available.
- Keep one configurable default list priority in the library. Before an operator
  saves an order, group lists by category, with uncategorized lists last. This
  initializes a new profile or forecast only when that request supplies no order; every profile
  then stores its own snapshot, and later library reordering does not rewrite
  existing profiles. Equal destinations belong to the first list in that profile;
  a lower-priority rule wholly covered by a higher list is omitted, while a
  broader lower-priority rule stays when it contributes unique addresses.
  Category references remain live and append newly resolved lists after the
  stored profile order.
- Show secrets only at their deliberate point of use. Subscription issuance
  follows first publication; stored hashes cannot recover the original token.
- Device changes require explicit authority, a compatible target, verified
  backup, bounded application, read-back, and recovery after failure. Keenetic
  static-route deletion additionally requires persisted exact ownership; shared,
  pre-existing, and unrelated same-interface profiles are preserved. Descriptions
  are verified exactly only for profiles that ownership says Routevane created.
  Scheduled delivery additionally needs an exact output/device binding and consent.
- Operator-installed plugins use the same validated publication/observation
  boundaries. Checksums and resource limits do not imply a hostile-code sandbox.
- Keep addresses, provenance, and reasons available through deliberate inspection;
  ordinary setup must not require network expertise.

## Acceptance map

| Requirement | Executable evidence |
| --- | --- |
| Deterministic policy and expiration | `internal/planner/*_test.go`; `cmd/routevane/observation_expiry_e2e_test.go` |
| Multi-format publication and previous-valid continuity | `cmd/routevane/every_format_e2e_test.go`; `cmd/routevane/two_formats_outage_e2e_test.go` |
| Library ownership and profile preservation | `cmd/routevane/library_removal_e2e_test.go`; `cmd/routevane/list_archive_e2e_test.go` |
| Discovery and learning boundaries | `cmd/routevane/service_from_url_e2e_test.go`; `cmd/routevane/learning_session_e2e_test.go` |
| Explicit device delivery | `cmd/routevane/device_deploy_e2e_test.go`; `cmd/routevane/scheduled_delivery_e2e_test.go` |
| Real plugin examples | `cmd/routevane/external_plugin_e2e_test.go` consumes their shipped manifests |
| Keyboard, localization, and browser flows | `web/tests/e2e/` against the built binary |
| Desktop lifetime, isolation, persistence and tray behavior | `tools/dev.ps1 test-desktop` against the native package; `cmd/routevane/desktop_test.go` |
| Windows application updates | `tools/dev.ps1 test-update`: manual installer replacement and explicit download/install/restart preserve profiles and preferences; corrupt downloads cannot install |
| New-user installation | Native release jobs start the packaged launcher with fresh data and check version, health, and UI |
| Documentation and file ownership | Structural validator plus the prerelease file/audience audit; tests alone do not establish clarity |

These are acceptance entry points, not a substitute for reading an actual run's
results. Device doubles and parser tests do not prove physical-device acceptance.

## Open work and limits

- Signed desktop installers and a separately managed background service are not
  yet delivered. The CLI remains separately distributable. Native desktop
  acceptance is recorded per OS; Windows evidence does not establish macOS/Linux
  tray or installer behavior.

- Physical Keenetic, OpenWrt, MikroTik, and Amnezia acceptance, including device
  interruption/recovery where relevant, remains unverified. Local sing-box
  delivery proves file validity, not live runtime reload.
- Subscription fetch observation, token rotation/revocation, interface choices
  returned by device probe, and a user-facing build-history browser remain
  unimplemented.
- Per-profile exclusion of individual observed values is not implemented; profile
  composition includes or excludes whole lists (ADR 0029).
- One product vocabulary is adopted and lands as ordered slices in a breaking
  `0.2.0` (ADR 0039). The executable, the HTTP API, the browser addresses, the
  interface dictionary and the catalog keys now say `list` for a set of rules
  and `profile` for the composition that publishes it, and the configuration
  transfer format states it at `config-transfer-v1.4`. Go identifiers and
  SQLite table names still say `service` and `list`; documentation keeps
  explaining that mapping until they land. The retired catalog keys and
  transfer field names are read for one minor version, and a file naming one
  thing under both a current and a retired name is refused.
- Scaling changes are conditional on measured workload.
  PostgreSQL, separate workers, and telemetry infrastructure are not planned
  merely to complete a checklist.
- A local clean clone is not proof of a clean OS or hosted publication. Report
  tool/browser caches, OS matrix, remote publication, and hardware evidence separately.
