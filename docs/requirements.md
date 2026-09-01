---
status: adopted
---

# Current product requirements

Audience: contributors deciding what Routevane must preserve. Installation belongs
in the [user guide](../README.md); implementation belongs in
[architecture](ARCHITECTURE.md) and the [UI contract](UI.md).
The original [implementation plan](history/implementation-plan.md) remains
historical requirements evidence. This document records its current interpretation,
including later [decisions](adr/README.md), not a claim that every proposed feature shipped.

## Product contract

- Run one local executable with an embedded English/Russian UI and API. No cloud
  account, separate worker, or external database is required.
- A **list** contains destinations; a **category** groups lists; a **route**
  composes them and publishes through one or more **connections**. In the API,
  `services` still means UI lists and `lists` means UI routes.
- Prefer safe domain-capable rules when the target supports them. DNS evidence
  never justifies WHOIS/RDAP/ASN expansion. Observation validity and policy
  acceptance are separate; decisions are deterministic and reason-coded.
- Keep published plans and files immutable, validate before publication, and
  preserve the previous verified file when rebuilding fails.
- Separate global library editing from per-route selection. Permit library
  removal only when stored routes are not broken. Routes are archived, not
  destructively deleted; published history remains available.
- Show secrets only at their deliberate point of use. Subscription issuance
  follows first publication; stored hashes cannot recover the original token.
- Device changes require explicit authority, a compatible target, verified
  backup, bounded application, read-back, and recovery after failure.
  Scheduled delivery additionally needs an exact output/device binding and consent.
- Operator-installed plugins use the same validated publication/observation
  boundaries. Checksums and resource limits do not imply a hostile-code sandbox.
- Keep addresses, provenance, and reasons available through deliberate inspection;
  ordinary setup must not require network expertise.

## Acceptance map

| Requirement | Executable evidence |
| --- | --- |
| Deterministic policy and expiration | `internal/planner/*_test.go`; `cmd/routing-agent/observation_expiry_e2e_test.go` |
| Multi-format publication and previous-valid continuity | `cmd/routing-agent/every_format_e2e_test.go`; `cmd/routing-agent/two_formats_outage_e2e_test.go` |
| Library ownership and route preservation | `cmd/routing-agent/library_removal_e2e_test.go`; `cmd/routing-agent/list_archive_e2e_test.go` |
| Discovery and learning boundaries | `cmd/routing-agent/service_from_url_e2e_test.go`; `cmd/routing-agent/learning_session_e2e_test.go` |
| Explicit device delivery | `cmd/routing-agent/device_deploy_e2e_test.go`; `cmd/routing-agent/scheduled_delivery_e2e_test.go` |
| Real plugin examples | `cmd/routing-agent/external_plugin_e2e_test.go` consumes their shipped manifests |
| Keyboard, localization, and browser flows | `web/tests/e2e/` against the built binary |
| New-user installation | Native release jobs start the packaged launcher with fresh data and check version, health, and UI |
| Documentation and file ownership | Structural validator plus the prerelease file/audience audit; tests alone do not establish clarity |

These are acceptance entry points, not a substitute for reading an actual run's
results. Device doubles and parser tests do not prove physical-device acceptance.

## Open work and limits

- Physical Keenetic, OpenWrt, MikroTik, and Amnezia acceptance, including device
  interruption/recovery where relevant, remains unverified. Local sing-box
  delivery proves file validity, not live runtime reload.
- Subscription fetch observation, token rotation/revocation, interface choices
  returned by device probe, and a user-facing build-history browser remain
  follow-up requirements from the historical UI brief.
- Per-route exclusion of individual observed values is not implemented; route
  composition includes or excludes whole lists (ADR 0029).
- Renaming the legacy API/Go identifiers is separate compatibility work
  (ADR 0028). Current documentation must explain the mapping meanwhile.
- The historical scaling milestone is conditional on measured workload.
  PostgreSQL, separate workers, and telemetry infrastructure are not planned
  merely to complete a checklist.
- A local clean clone is not proof of a clean OS or hosted publication. Report
  tool/browser caches, OS matrix, remote publication, and hardware evidence separately.
