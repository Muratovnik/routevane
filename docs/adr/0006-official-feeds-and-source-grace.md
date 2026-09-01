---
status: adopted
---

# ADR 0006: official network feeds, destination policy, and source grace

## Context

Milestone 5 adds a second real source. A network feed differs from DNS in three
ways that change policy rather than plumbing.

A feed is fetched from a URL the catalog supplies, so the destination is
attacker-influenced input. Routevane runs on the operator's own machine beside a
loopback API and a bearer-token subscription store, which is exactly the shape
server-side request forgery targets.

A feed publishes networks, not single addresses. Auto v1 quarantines observed
prefixes because one observation never proves that a whole network belongs to a
service. That reasoning does not transfer unchanged to a list the network's own
operator publishes.

A feed can be unavailable. Under the M1..M4 rules an unreachable source silently
drops every route it contributed as soon as the observations expired, which turns
a transient outage into a broken artifact.

## Decision

- The feed source accepts only an absolute HTTPS URL with no credentials, no
  fragment, and no opaque form. The catalog rejects a bad URL at load time, so a
  malformed feed entry fails before the first refresh.
- The transport resolves the host itself and refuses the connection when **any**
  returned address is outside the public unicast policy. Refusing the whole
  answer, rather than picking an allowed address out of a mixed answer, is what
  makes a split DNS answer unusable for rebinding. The policy rejects
  unspecified, loopback, private, unique-local, link-local (including the
  169.254.169.254 metadata address), carrier-grade NAT, protocol-assignment,
  benchmarking, multicast, IPv4-mapped, 6to4, Teredo, and NAT64 destinations.
  Documentation ranges stay allowed: they are not a local trust boundary, and
  tests need an address class that is neither local nor a real internet host.
- Proxy support is deliberately absent. An environment proxy would move the
  destination decision outside the validated address policy.
- Every redirect hop is validated again by the same URL check and the same dial
  policy, hops are bounded, and `Authorization`/`Cookie` are stripped before a
  hop. Response bytes, entry count, and time are bounded; an oversized feed is
  refused rather than truncated, because a truncated feed is silently incomplete.
- An entry that does not normalize to a canonical address or prefix is counted as
  skipped and never recorded. A feed never contributes an unchecked string.
- A prefix from `official` source class routes with reason `official_rule`. An
  operator publishing its own network list is a declaration by the owner, not an
  inference. Prefixes from observed, community, or metadata classes stay
  quarantined with `wide_network_expansion`, and explicit trusted
  shared-network evidence still quarantines an official prefix with
  `shared_cdn_or_cloud`, because a shared CDN range is not service specific.
- Source health is read in the same transaction as the observations it explains.
  A source whose latest cycle failed while its previous success is still inside
  `SourceGracePeriod` is degraded: its expired observations stay routable, each
  such rule carries `source_degraded` and expires with the grace window, and the
  plan carries a `source_degraded:<service>:<source>` warning. Grace never
  revives an archived or invalid observation, so it cannot resurrect data the
  retention policy dropped.
- Lossless collapse now carries the union of the merged rules' reason codes and
  their latest expiry instead of rebuilding a fixed reason. A collapse that
  dropped `source_degraded` would have changed policy silently.
- Source type is bound to an implementation in exactly one composition registry,
  so a catalog source type cannot be supported by one entry point and
  unsupported by another. Each source type keeps only its own configuration: a
  DNS entry cannot carry a URL and a feed entry cannot carry DNS names.

## Consequences

The address policy is intentionally stricter than "not obviously internal".
A feed hosted inside the operator's own network is not reachable, and that is
the trade this decision accepts; a future milestone that needs it must add an
explicit, narrowly named local-feed permission rather than widening this policy.

Grace changes what a published artifact means: a route may outlive its
observation by up to the grace window. The reason code and the plan warning are
what keep that visible, so removing either would make the artifact dishonest.
The window is a source-level decision because feeds carry no per-entry TTL.

Accepting official prefixes as routes makes the artifact considerably larger for
services that publish wide ranges. The target rule limit, not the feed, is what
bounds that, and exceeding it still fails the build with a diagnostic plan
instead of a truncated one.

Rolling back the feed source means removing `type: http` entries from the
catalog; stored feed observations expire on their own because their source
revision no longer matches an active source.

A degraded refresh is reported as a warning rather than an error, because
refusing it would leave the user with no artifact at all while usable
observations were on disk. Once a grace window closes the same failure becomes an
error again, and an explicit build still publishes what the healthy sources
support.
