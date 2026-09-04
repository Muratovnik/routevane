---
status: adopted
---

# ADR 0032: persist exact Keenetic static-route ownership

This supersedes only ADR 0008's decision that interface membership establishes
Routevane ownership and its exact-interface verification rule. ADR 0008's
destination, credential, lifecycle, backup, rollback, protocol, and acceptance
decisions remain adopted.

## Context

The original Keenetic reconciliation treated every static route attached to the
selected interface as Routevane-owned. That made a later artifact able to delete
an operator-created route merely because it used the same VPN or WAN interface.
The router does not expose a durable creator identity for a static route, so the
deployer cannot recover exact ownership from the route table.

An output can be delivered manually or by the scheduler, and several outputs can
need the same prefix. Ownership therefore cannot be request-local, tied only to a
registered device ID, or reconstructed from the latest artifact.

## Decision

- SQLite stores active ownership behind an application-consumed repository. A
  scope is the canonical device endpoint, catalog target, and device interface.
  Each scope stores output-to-prefix claims separately from whether Routevane
  created the physical prefix.
- Manual and scheduled delivery resolve the artifact's stored output identity
  and use the same deployment service and ownership repository. A registered
  device ID and a credential are not part of ownership identity.
- Reconciliation replaces only the current output's claims. A physical prefix
  is added when any claim needs it and the router does not report it. It is
  removed only when no claim remains and the ledger says Routevane created it.
- A desired prefix already present when first claimed is recorded as
  pre-existing. It is never deleted from that evidence, even after its last
  claim disappears. Other unclaimed routes on the interface are ignored.
- A missing active ledger is additive. The current route table may prove that a
  desired prefix is present, but it never proves deletion authority.
- Deployment keeps the fixed probe, compatibility, verified-backup, bounded
  apply, read-back, and rollback lifecycle. Verification requires every claimed
  prefix to be present and every prefix authorized for removal by this attempt
  to be absent; unrelated extra routes are accepted.
- The next ledger snapshot is committed atomically only after apply and
  read-back verification succeed. A deploy or verification failure rolls back
  the router and never writes the ledger. If the ledger commit fails after a
  physical change, the router is restored from the verified backup.
- Changing stored connection metadata or forgetting a registered device retires
  the old scope without contacting the router. Retired rows remain historical
  and cannot authorize a later deletion if an endpoint is reused.
- Keenetic implements an optional exact-route deployer capability but does not
  import SQLite. The renderer continues to know nothing about device state, and
  other deployers keep the generic deployment boundary.

## Consequences

Exact cleanup depends on successfully persisted history. Losing or retiring that
history deliberately favors an extra route over deleting an unknown one. A route
created by Routevane but no longer represented by an active ledger may therefore
need manual cleanup.

The ledger is product state but contains no credential or router configuration.
Full configuration backups remain the rollback mechanism, so operators must
still avoid concurrent router changes during deployment.

The behavior is verified with SQLite reopen/retirement tests, application set
transition tests, and the Keenetic device double. Physical Keenetic acceptance
remains unverified.
