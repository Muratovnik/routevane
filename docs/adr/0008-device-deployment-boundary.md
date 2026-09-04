---
status: adopted
---

# ADR 0008: device deployment boundary

## Context

Milestone 8 applies an already-published artifact to the operator's own router.
Three facts shape the decision more than the code does.

A router lives at a private address, so deployment is the one operation in this
product that must reach the local network — exactly what every other boundary
refuses. `.agents/rules/security-boundaries.md` allows that only for a narrowly
named local operation.

Deployment writes to a device the operator depends on for connectivity. A
partial write, an unsupported firmware, or a lost route is not a failed request:
it is a device that no longer works. Ordering is therefore part of the contract,
not an implementation detail.

The Keenetic RCI interface this deployer speaks is **not** documented by a vendor
specification. The vendor's developer pages redirect to a support portal, and the
authentication exchange and command shapes are described only by community
sources. That is a material verification limit, not a detail.

## Decision

ADR 0032 supersedes only the two static-route ownership and exact-interface
verification decisions below. All other decisions in this ADR remain adopted.

- `netpolicy.DeviceDestination` is the narrowly named exception to the public
  unicast policy. It permits private and link-local unicast only, and continues
  to refuse loopback, the unspecified address, multicast, carrier-grade NAT, and
  the cloud metadata address, so widening it cannot reach a service on the
  machine itself. A mixed answer is refused whole, exactly as the public policy
  refuses one.
- A device URL must be an address literal. A hostname is refused because a device
  address must be unambiguous when the operator types it, and resolving one would
  make the destination depend on a name server the router itself may be serving.
  Redirects are refused outright.
- The device password is read only from `ROUTEVANE_DEVICE_PASSWORD`. It is never
  a flag, because a flag appears in the process list and in shell history.
  `Connection` carries the credential and is passed to every deployer call;
  `DeviceInfo`, `BackupRef`, and the audit record never can, which is why the
  plan's illustrative signature — where later calls take only the device — is
  deliberately not followed.
- The lifecycle order is fixed: probe, compatibility, backup, deploy, verify, and
  rollback when either deploy or verify fails. Each rule exists because skipping
  it makes a failure unrecoverable. An unsupported firmware reports an empty
  profile key rather than an error, so the refusal names the version actually
  found. A backup must be produced, stored, read back, and hash-verified before
  a deployment starts.
- **Superseded for Keenetic static routes by ADR 0032:** Verification reads the device's own route table back and compares it with the
  artifact. A deployment is applied only when the device holds exactly the
  artifact's routes on the named interface; the answer names what is missing and
  what is unexpected.
- **Superseded for Keenetic static routes by ADR 0032:** Idempotence comes from the shape of the operation rather than a comparison of
  timestamps: the deployer removes exactly the routes it previously owned on the
  named interface and adds exactly the artifact's routes. A route the operator
  added on another interface is never touched, and a repeated deployment issues
  no command batch at all.
- The deployer validates the artifact with the renderer's own parser and never
  renders, never plans, and never sees a `RoutingPlan`. Rendering and deployment
  stay separate packages: the deployment path installs bytes the publication path
  already proved.
- A device answers 200 even for a rejected command, so the answer body decides
  success. Every step is bounded in time and bytes, and the audit trail records
  each step's identity, outcome, and duration for a failed attempt as well as a
  successful one.
- `InsecureDeviceTLS` is opt-in per deployment and applies only to a private
  address the device policy already accepted. A router cannot hold a publicly
  trusted certificate for its LAN address; this is the only boundary in the
  product where verification is relaxed.

## Two findings the peer protocol forces

The RCI challenge scheme is defined with MD5 by the device, and a router cannot
hold a publicly trusted certificate for its LAN address. Both are properties of
the peer, not choices this code makes, and neither can be coded away while
speaking to the device at all.

`gosec` therefore honors a `#nosec` annotation again, but only with a written
justification (`-nosec-require-justification`). The three annotations in this
deployer are the only ones in the repository: the MD5 import, the MD5 digest, and
the opt-in device TLS relaxation. The MD5 value is a wire-format transformation,
never a password store, and the TLS relaxation applies only to a private address
the device policy already accepted.

That is a deliberate, reviewable exception rather than a lowered gate: an
annotation without a justification is still refused, and the gate still scans
every other package for the same rules.

## Verification limits

The deployer is verified against a faithful device double that reproduces the
community-documented challenge exchange, command batching, error-in-200
behaviour, route table, and configuration restore. The mandatory tests all run
against it: an unsupported firmware is refused before anything changes, a
deployment never starts without a verified backup, a failed verification rolls
back, a repeated deployment is idempotent, renderer and deployer stay separate
packages, and no credential reaches an error, an audit record, or the device in
cleartext.

**Live acceptance against a physical Keenetic router has not been performed in
this environment.** Because the protocol is community-documented, a firmware
whose RCI surface differs will fail at probe or at verification rather than
silently: the deployer refuses what it cannot confirm. An operator's first real
deployment is still the acceptance step, and the manual artifact download remains
the supported path that needs no device credentials.

## Consequences

Automatic deployment is opt-in and requires a device account. The manual BAT
import continues to work unchanged, so a router this deployer cannot speak to is
not a regression.

Adding a second deployer means adding a `Deployer` implementation and one entry
in the deployment registry, keyed by renderer id because what can be installed is
decided by the format. The lifecycle, the audit trail, the backup store, and the
destination policy are shared and must not be re-implemented per device.

If a vendor specification becomes available and contradicts the community
description, this deployer's transport changes and its ADR is superseded; the
lifecycle contract above does not.
