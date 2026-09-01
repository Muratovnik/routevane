---
status: adopted
---

# ADR 0021: Routevane owns the FQDN groups it wrote, and removes them

## Context

ADR 0017 added the `keenetic-dns` target and recorded the gap it left open:
reapplying a changed list leaves the previous `routevane-*` object groups on the
router, and after a few refreshes they exhaust the 128-group budget. A list that
drops Netflix keeps routing Netflix forever.

That ADR also stated that the generated file carries the removal commands for
the groups it replaces. It never did, and it cannot. The artifact is a function
of the plan alone — that is what makes its hash identify its content — so it
cannot know what the device already holds. And the one command that would clear
a group, `no object-group fqdn`, takes the operator's own `dns-proxy route` with
it, so a file that deleted before writing would silently stop the routing it was
meant to install.

Only something that can read the device can reconcile it. That is the deployer.

## Decision

- **A deployer for `keenetic-dns`**, alongside the static-route one. It shares
  the RCI transport, the authentication, the destination policy, the backup and
  the rollback, because those are properties of the router rather than of the
  artifact format. It does not share the firmware gate: DNS-based routes exist
  from 5.0.1, below the 5.0.4 floor the route surface needs, so each deployer
  states its own minimum and makes its own interface check.
- **Ownership is the `routevane-` prefix and nothing else.** Device state is
  filtered at the single point where it enters the deployer, so no later step
  can act on a name we did not create. A group the operator made, and a
  `dns-proxy route` pointing at it, are invisible to us.
- **Within that boundary we own the whole lifecycle**, including the
  `dns-proxy route` of each group. Removing a stale group requires withdrawing
  its route first, so a deployer that can unbind must also bind; otherwise it
  would destroy an operator's binding on cleanup and never restore one.
- **Idempotence comes from the shape of the operation**, not from comparing
  hashes: read what we wrote, remove what the artifact no longer names, add what
  is missing, point each group at the named interface. Applying the same
  artifact twice sends nothing the second time.
- **The order is a correctness requirement, not tidiness.** Routes are withdrawn
  before the groups they name, because the device refuses to delete a group in
  use. Entries are removed before new ones are added, so a heavily changed group
  never briefly exceeds the per-group bound. A route is attached last, when its
  group is complete, so a resolved answer is never sent to a half-filled group.
- **The command surface is the RCI `parse` endpoint** — `{"parse": "<command
  line>"}` — rather than a structured JSON tree. The object-group commands have
  a published command-line form and no published JSON form, and speaking the
  documented one is better than inventing the other.
- **The manual path adds and updates but does not remove**, and the target says
  so. A file cannot reconcile without either knowing the device or destroying
  bindings, and neither is acceptable in something a person pastes.

## Consequences

Routevane now owns objects on someone's router, which it did not before. The
prefix is the entire safety boundary, so it is enforced where device state is
read rather than at each call site, and the group name validator in the renderer
already refuses to produce a name without it.

A batch that fails part way leaves the device partly written. The deployer
cannot make it atomic, because the device applies each command as it reads it.
That is why this target keeps the existing deployment order: backup, deploy,
verify against the device's own answers, and roll back the captured
configuration when verification fails.

Verification is exact in both directions. A group the artifact does not name,
found under our prefix after a deployment, fails the deployment as loudly as a
missing one: it means reconciliation did not do what it said.

Two facts are taken from a maintained third-party client rather than from vendor
documentation: the read paths `/rci/object-group/fqdn` and `/rci/dns-proxy/route`
with their answer shapes, and the `auto` keyword on a `dns-proxy route`, which
corresponds to the "Add automatically" option the device's own DNS-based routes
page offers. ADR 0003's rule applies unchanged: if documentation or hardware
disproves them, disable the target and supersede this ADR.

Hardware acceptance remains unverified for this target, as it is for BAT. The
device double in the tests refuses to delete a group that is still routed, which
is the behaviour the ordering rule exists for, but a double agreeing with our
reading of the protocol is not the protocol agreeing with it.

## Sources

- [gokeenapi](https://github.com/Noksa/gokeenapi) — the maintained client whose
  `dns_routing` implementation supplies the read paths, the command forms and
  the 300-entry-per-group figure.
- [DNS-based routes](https://support.keenetic.com/explorer/kn-1613/en/51150-dns-based-routes.html)
  — the device page whose "Add automatically" option `auto` corresponds to.
