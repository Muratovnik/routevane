---
status: superseded
---

# ADR 0012: the device credential through the local API

> Superseded 2026-08-22 by
> [`0014-opt-in-device-credential-storage.md`](0014-opt-in-device-credential-storage.md),
> as this ADR's own closing paragraph required. Everything here still describes
> manual delivery, which ADR 0014 carries over unchanged; only the refusal to
> ever store the credential is replaced, and only for a device whose operator
> explicitly enabled automatic delivery.

## Context

Milestone 8 made deployment a command-line operation and read the device
password from one environment variable, never a flag, because a flag appears in
the process list and in shell history
([`0008-device-deployment-boundary.md`](0008-device-deployment-boundary.md)).

That left the product with a gap an operator falls into every time. The browser
screen takes them from choosing services to a validated, published file. Then it
stops. Applying that file meant leaving for a terminal, finding the artifact
identity — which only the API reports — setting an environment variable, and
assembling a command. The step that changes the router was the one step with no
help at all, which is the opposite of where help belongs.

Closing that gap means the device password crosses a boundary it did not cross
before: from a browser page, through an HTTP request, into the process. That is a
real change, not a convenience, and it is the reason this ADR exists.

## Decision

Deployment is available from the local control surface. The credential is
supplied per attempt, travels once in a request body over IPv4 loopback, and
stops at the deployer.

- **Nothing stores it.** The password is read from the request body, passed to
  the deployer for that one call, and dropped. It is never written to the
  database, never put in a file, never returned in a response, and never placed
  in the URL. The page clears its own field as soon as the attempt ends —
  success or failure — and keeps nothing in `localStorage` or `sessionStorage`,
  which the browser test asserts directly.
- **Nothing logs it.** The request log records the route and the method. The
  audit record carries step identities, outcomes, and durations; `DeviceInfo`,
  `BackupRef`, and every returned value are structurally incapable of holding a
  credential, which is why `Connection` was a separate argument from the start.
  A test asserts the password appears in neither the response nor the log, and
  that the device address does not appear in the log either.
- **The existing mutation guard is the authorization.** A deployment is a POST,
  so it already requires a loopback client, `Content-Type: application/json`,
  the `X-Routevane-Request: 1` marker, and an `Origin` that matches this
  server's own. A page on another origin cannot send that combination, and a
  form post cannot send that content type. The environment variable remains the
  only source for the command line.
- **Two steps, not one.** Without `confirm` the request returns what would
  happen — the artifact, its target, its deployer, and what the connection must
  carry — and contacts nothing. The change requires a second, explicit request.
  This is the same stop the command line's `--confirm` enforces, and the browser
  test proves the file is absent between the two.
- **The deployer decides what a connection needs.** `Deployer.Requirements`
  declares whether this transport authenticates and whether it attaches to a
  named interface, and the screen builds its form from that. A destination on
  this machine therefore never shows a password field, and supplying one anyway
  is refused rather than ignored. An operator is never asked for a secret that
  has nowhere to go.
- **The lifecycle is unchanged.** Probe, compatibility, backup, deploy, verify,
  and rollback on failure, with the same refusals in the same order. The API is
  a second caller of the same use case, not a second implementation of it, and a
  failed attempt returns its audit trail because a failure is exactly the case
  an operator needs the record of.
- **Publication and deployment stay separate.** The deployment service is
  composed alongside the publication service rather than inside it, and the two
  are presented to the HTTP layer by one adapter that adds no behavior.
  Publication owns formats; deployment owns transports
  ([`0011-routing-plan-renderer-boundary.md`](0011-routing-plan-renderer-boundary.md)).

## Consequences

The product now has one place where the whole task happens, which is what makes
the earlier milestones usable rather than merely correct.

The accepted risk is precise: any process that can reach loopback, satisfy the
mutation guard, and already knows the device address and its password can
trigger a deployment. It cannot learn the credential from this product, because
nothing here retains it — it must arrive with the request. That is the same
position the command line was already in with an environment variable, moved to
a transport with an origin check.

What is deliberately not done: no credential storage, no "remember this device",
and no unattended or scheduled deployment. Each of those would require keeping
the secret, which is the one thing this decision refuses. A proposal to add one
replaces this ADR rather than extending it.
