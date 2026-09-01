---
status: adopted
---

# ADR 0004: immutable local publication and opaque subscriptions

## Context

Milestone 3 needs a stable local URL without allowing a failed renderer, a
partial database write, or a damaged latest file to erase the last usable
Keenetic artifact. The URL is a bearer capability: browser history, request
targets, logs, and database recovery must not turn it into a routinely exposed
profile field.

The earlier `profiles` table is an effective per-service observation watermark,
not the product profile introduced by this milestone. Reusing that name would
mix refresh state with publication ownership and make later API semantics
ambiguous.

## Decision

- Schema version 2 renames the M1 table to `effective_profiles` and creates
  product `profiles`, immutable `plan_snapshots`, immutable `artifact_builds`,
  and one `subscriptions` row per product profile.
- A snapshot stores the exact bounded canonical RoutingPlan JSON produced before
  renderer invocation. Its random ID is identity; semantic hash remains planner
  output and deliberately excludes time, database IDs, and subscription data.
- A validated artifact is written and durably verified under
  `artifacts/published/<renderer>/<sha256>.bat` before one SQLite transaction
  inserts or verifies the immutable records and advances `previous` and
  `latest`. A database failure may leave an unreachable content-addressed file,
  but cannot publish it. Milestone 3 does not add garbage collection.
- Subscription tokens are `rv1.<128-bit token id>.<256-bit secret>`. SQLite
  stores only the indexed token ID and SHA-256 of the complete token. Lookup
  performs a constant-time hash comparison. The complete local URL is returned
  only by profile creation and is never returned by profile/status reads.
- Subscription reads verify the expected canonical path, regular-file identity,
  size, and SHA-256. A missing or corrupt latest file causes an independently
  verified previous artifact to be served with
  `X-Routevane-Fallback: previous`; reads never change pointers.
- `serve` binds only `tcp4 127.0.0.1`, owns the existing long-lived process lock,
  and exposes the M3 API and a server-rendered no-script status page. Host,
  remote address, origin, media type, CSRF marker, bytes, JSON shape,
  concurrency, headers, and timeouts are bounded at the HTTP boundary.
- Artifact ETag is the quoted payload SHA-256. Last-Modified is the earliest
  immutable creation time for those exact bytes, so a later snapshot with the
  same payload does not create a false cache change.

## Consequences

Published plan and build rows cannot be updated or deleted; changing their
shape or pointer semantics requires a new migration. Product profiles are
create-only in M3. There are no channels, device pins, history UI, deployer,
renderer registry, failed-build table, diff table, or artifact garbage
collector.

The loopback API is not remote authentication. The bearer URL should still be
handled as a secret. A user who loses it creates another profile in this
milestone; plaintext recovery is intentionally impossible.

Rollback disables `serve` and restores a version-1 database from backup. It
must not rewrite version-2 immutable rows in place. If later acceptance needs
remote access or token rotation, adopt a separate authenticated design and
migration rather than widening this loopback contract.
