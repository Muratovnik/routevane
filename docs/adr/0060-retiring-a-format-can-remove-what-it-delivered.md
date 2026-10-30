---
status: adopted
---

# ADR 0060: retiring a format can remove what it delivered

This decision replaces the "Router state is not touched" clause of
[ADR 0051](0051-profile-and-format-retirement-and-subscription-reissue.md) for a
retirement that asks for it. A retirement that does not ask is unchanged. The
ownership rules of [ADR 0032](0032-persist-exact-keenetic-static-route-ownership.md)
and [ADR 0040](0040-exact-fqdn-output-ownership.md) and the device lifecycle of
[ADR 0008](0008-device-deployment-boundary.md) and
[ADR 0020](0020-a-rollback-outlives-the-request-that-triggered-it.md) are
unchanged.

## Context

Removing a format and deleting an archived profile left on the router everything
Routevane had delivered there: Keenetic FQDN groups with their
`dns-proxy route object-group … <interface>` attachments, and static routes for
the BAT format. The operator had to find and remove them by hand, although
Routevane keeps an exact ledger of what it created for each format on each
router. The ledger rows of the retired format stayed too.

ADR 0051 kept router state for two reasons. Removing an FQDN group needs the
device. Removing a static-route claim without the route lets the next send of
another format delete a route the operator never asked to remove. Both still
hold; the decision below removes the objects with the device, under the same
lifecycle as a send, and removes the claims in the same step.

## Decision

- **The operator opts in per act.** `POST /v1/outputs/{id}/remove` and
  `POST /v1/profiles/{id}/delete` accept `{"remove_from_devices": true}`. The body
  `{}` keeps ADR 0051 exactly: no device is contacted, and the ledger rows stay as
  evidence that the objects on the router are Routevane's.
- **The ledgers decide what is removed, and nothing else is.** For FQDN groups it
  is the groups the format's ledger names on that router, each route withdrawn
  before its group. For static routes the format's claims go, and a route goes
  only when Routevane created it and no other format claims it; a route the
  router had before Routevane claimed it stays. A group or route outside the
  ledger is never read as removable, whatever its name.
- **A read shows it first.** `GET /v1/outputs/{id}/device-objects` and
  `GET /v1/profiles/{id}/device-objects` list, for each router surface a format
  delivered to, the saved connection that would be used, whether it holds a saved
  password, and the group names or the counts of claimed, removable, shared and
  pre-existing routes. The read never contacts a device. A screen can show what
  stays on the router whether or not the operator asks to clean it.
- **Only a saved connection with a saved password cleans.** A connection matches
  a surface when its own deployer canonicalizes it to the ledger's surface: the
  same target and address and, for static routes, the same interface. FQDN groups
  are router-wide, so any interface of the matching address serves. Among several
  matches, one with a saved password comes first, then the format's bound
  connection, then the oldest. The request carries no password: the choice is
  one checkbox, and a saved password is already the operator's consent to its
  use for that router ([ADR 0050](0050-saved-connection-credential.md)).
- **The act is all or nothing, as far as several routers allow.** A surface
  without a matching connection and saved password refuses the whole act with
  `device_cleanup_unavailable` before any device is contacted, and the refusal
  names each such surface. Cleaning the reachable routers and retiring anyway
  would remove the format while its objects stay on another router, with nothing
  left to say so. Every router is then asked read-only — probe, firmware check
  and, for FQDN groups, a read that refuses an owned object the deployer cannot
  remove safely — before any is changed, so a refused password on the second
  router leaves the first untouched. Each surface then runs the send lifecycle:
  verified backup, the removal (step `remove`), read-back, ownership commit, and
  a rollback on its own budget when the change may have happened and did not
  verify. The format or profile is retired only after every removal verified.
  A refusal at any point retires nothing and answers the stable codes a send uses
  (`device_auth_rejected`, `device_unreachable`, `verify_failed`, …) with the step
  and the surface it stopped at.
- **The ledger rows go with the objects, one surface at a time.** The ownership
  step after read-back removes the format's rows on that surface, and a failed
  write restores the router from the backup. Removing the rows only after the
  retirement would leave, between the two, a claim on a route the router no
  longer holds, and another format's next send would put the route back. When a
  later surface fails, the earlier ones stay clean and their rows are gone; the
  format stays live, and its next send restores them. Repeating the act converges,
  because a surface whose rows are gone needs no device.
- **Each removal is recorded in its own journal.** Schema version 22 adds
  `device_cleanup_attempts`: the format, the saved connection, the router address,
  the audited steps and the outcome, claimed before the first device effect and
  completed once, with the immutability triggers of `deployment_attempts`. The
  deployment journal does not fit: it is keyed by an artifact a removal does not
  apply, a success there means the artifact was applied, and a profile page reads
  its successful rows as deliveries. The server generates the attempt identity;
  a client-generated one (ADR 0042) is not needed, because a repeated request
  finds nothing left to remove instead of replaying a change.
- **The routes get a device budget.** Both retirement routes carry a five-minute
  budget, inside the desktop transport's ten-minute abort, because the opt-in runs
  the send lifecycle once per surface. The request holds the delivery gate through
  every removal and the retirement after it.
- **The deployer boundary stays.** The application declares `ManagedFQDNRemover`;
  the Keenetic DNS deployer implements it. Static-route removal is the existing
  exact mutation of the Keenetic route deployer, computed by the application from
  the shared claims, as a send's is. The local sing-box deployer keeps no ledger,
  so it never delivered anything Routevane could name, and nothing is listed or
  removed for it.

## Alternatives

- **Clean what can be reached and retire anyway.** Rejected for the reason above;
  the read already tells the operator which routers block the act, and saving a
  password for that connection or not asking to clean are both one step away.
- **Remove the ledger rows on retirement without the device.** Rejected for the
  reasons ADR 0051 gives.
- **Add a kind column to the deployment journal.** It would change the delivery
  read and the meaning of an artifact identity for every existing row. A table of
  its own changes neither.

## Consequences and verification limits

A version-22 database cannot be opened by an earlier binary; rollback restores a
backup taken before the upgrade. A cleanup interrupted by process death leaves its
journal row pending, which reads as an unknown outcome, as for a send.

The behaviour is verified against the Keenetic device doubles end to end through
the HTTP API, SQLite and the real deployers: owned groups removed and another
format's and the operator's kept, static routes removed except those another
format claims, a refused password and a failed read-back retiring nothing, and a
retirement without the opt-in unchanged. Physical router acceptance is unverified,
as it is for a send.
