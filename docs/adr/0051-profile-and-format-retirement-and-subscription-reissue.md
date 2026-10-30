---
status: adopted
---

# ADR 0051: retiring a profile or a format, and reissuing a subscription link

This decision replaces one clause of
[ADR 0018](0018-archived-lists-are-frozen-not-hidden.md): archival is no longer
"deliberately not a deletion path". It also replaces the never-rotated token of
[ADR 0004](0004-immutable-local-publication.md) and the one immutable
subscription per output of
[ADR 0023](0023-subscriptions-begin-with-a-successful-publication.md) with an
explicit reissue. Publication immutability, archival as a frozen state, and the
issuance rules of both ADRs are unchanged.

The clause "Router state is not touched" is replaced by
[ADR 0060](0060-retiring-a-format-can-remove-what-it-delivered.md) for a
retirement that asks to remove what the retired formats delivered. A retirement
that does not ask still leaves router state and the ownership ledgers as below.

## Context

Nothing could leave Routevane for good. An archived profile kept its
composition forever, so every list and category it named directly stayed "in
use", and the library refused to delete them (ADR 0029). A format added by
mistake could not be removed from a profile. A subscription link that leaked
could not be withdrawn: the token was immutable and the only remedy ADR 0004
offered was a new profile.

The schema enforced all of it. `profiles`, `outputs` and `subscriptions` carry
triggers that refuse a delete, and `subscriptions` allowed one row per output.
Those triggers protect something real: plan snapshots, artifact builds,
attempts, the deployment journal and the router ownership ledgers all point at
these rows, and a deleted row would orphan that history.

The owner decided on 2026-10-13 that an archived profile can be deleted, a
format can be removed from a profile on the shelf, and a format's link can be
reissued.

## Decision

- **Retirement is a tombstone.** `profiles.deleted_at_ns` and
  `outputs.removed_at_ns` record the moment an object was retired, zero while it
  is live, in the same form as `archived_at_ns`. No row is deleted and no
  immutability trigger is dropped. Published files, plan snapshots, artifact
  builds, output attempts and deployment attempts stay exactly as they are, and
  disk space is not reclaimed.
- **Only an archived profile can be deleted.** `POST /v1/profiles/{id}/delete`
  answers `409` with code `profile_not_archived` for a profile on the shelf, and
  `404` for an unknown or already deleted one. Deletion stays the step after
  archival, so the operator has already seen the profile stop changing before it
  disappears.
- **Deleting a profile retires everything that answers for it, in one
  transaction.** Every token of its formats is revoked, every live format is
  retired with its connection binding cleared, and its composition rows are
  removed, so a list or category only it named can now leave the library. The
  schema refuses a deleted profile that still has a live format, a deletion of a
  profile that was never archived, and any later write to a retired row.
- **A format can be removed from a profile on the shelf.**
  `POST /v1/outputs/{id}/remove` retires one format with the same effects at
  format level. An archived profile is frozen, its formats included, so removal
  answers `409` with code `profile_archived`. The removed format's target can be
  bound again; the new output has a new identity, a new subscription and new
  router object names.
- **A retired object answers nowhere.** Reads by identity answer `404`. Profile
  lists, pages, the scheduler, the library's in-use check, the configuration
  export, a build and a deployment all skip or refuse it.
  `GET /v1/artifacts/{id}` and `GET /v1/snapshots/{id}` answer `404` for a
  retired format as well. No audit read goes through those addresses: the
  deployment journal keeps its own records, and both addresses exist to download
  or deploy a live format's file. Keeping them open would leave a deleted profile
  deployable by anyone who still had an artifact identity. The bytes and rows
  remain on disk.
- **The in-use refusal says which profiles are archived.** Deleting a list or a
  category that profiles still name answers `409`, and each profile in the body
  carries `archived_at`, the moment it was archived, omitted while it is live.
  An archived profile cannot be edited and is removed from the archive, so the
  screen chooses each profile's next step from the refusal itself and reads no
  profile to find out.
- **A subscription is one token among several over time.** `subscriptions` is
  keyed by token, and every token issued before this change is carried across
  unchanged. `subscription_revocations` records when a token stopped working and
  is insert-only. An output's active subscription is its newest unrevoked token.
  A request with a matching secret for a revoked token, or for a token of a
  retired format, answers `410 Gone` with code `subscription_revoked`. A wrong
  secret still answers `404`, so a token identity alone reveals nothing.
- **A link is reissued, not rotated in place.**
  `POST /v1/outputs/{id}/subscription/reissue` revokes the active token and
  stores a new one in one transaction, and returns
  `{"subscription_url": ...}`, the field a first successful build uses. The
  one-time-secret rules of ADR 0004 and ADR 0023 apply to the new token: only its
  identity and hash are stored, it is never logged, and no read returns it again.
  Reissue answers `409` `profile_archived` for an archived profile, `404` for an
  unknown or retired format, and `409` `subscription_not_issued` for a format
  that has never had a link. The first link still comes from the first
  successful build.
- **Router state is not touched.** Retirement does not contact a device. The
  static-route claims and FQDN ownership rows of a retired format stay as its
  last successful deployment left them. Removing a claim would let the next
  deployment of a sibling output delete routes the operator never asked to
  remove (ADR 0032), and removing an FQDN group needs the device (ADR 0040). The
  routes and groups stay on the router, protected by the retained ownership,
  until the operator removes them or the stored connection is retired.
- **Retirement shares the delivery gate.** Deleting a profile and removing a
  format clear output-to-device bindings, so they take the same gate as every
  binding change (ADR 0031). An automatic delivery either finishes first or reads
  the retired output afterwards and delivers nothing.

## Consequences

SQLite schema version 17 adds the two columns, rebuilds `subscriptions` around
its token, adds `subscription_revocations`, makes the one-output-per-format index
apply to live outputs only, and adds the triggers that make retirement permanent
and complete. A version-17 database cannot be opened by an earlier binary;
rollback restores a backup taken before the upgrade.

Deletion cannot be undone. There is no restore for a deleted profile, and a
reissued link cannot bring back the previous one.

Delivered router state can outlive the format that delivered it. A retired
format's static routes and FQDN groups keep routing until the operator removes
them from the router. Reconciling device state for a retired format is a separate
change.
