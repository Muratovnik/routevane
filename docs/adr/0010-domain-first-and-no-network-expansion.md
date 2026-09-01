---
status: adopted
---

# ADR 0010: domain-first observation and no network expansion

## Context

This decision has governed every milestone from the first spike onward, and it is
the one that decides whether the product is safe to run. It is recorded here
because the plan requires it as a minimum architectural document and because
every later boundary — the grace period, the discovery proxy, the dynamic-set
formats — is a consequence of it and unreadable without it.

The tempting design is the wrong one. A service is identified by names, and
resolving a name yields addresses. Once addresses are in hand, it is one small
step to ask who owns them — WHOIS, RDAP, an ASN lookup — and to route the
enclosing network. That step is what makes a routing product dangerous: one
observed address for one component of one service can pull in a hosting
provider's entire allocation, and with it every unrelated service behind the same
infrastructure. The operator asked for YouTube and silently got a third of the
internet, including services they deliberately route directly.

The failure is not hypothetical or gradual. It is a single rule, applied once,
that no later observation removes.

## Decision

Names are the primary identity. An address is evidence with a lifetime, never an
identity, and no observation is ever widened beyond itself.

- **A service is defined by names.** A catalog service carries seeds — exact
  domains and suffixes — and the components those names belong to. Addresses are
  observed for those names; they never define what the service is.
- **One observed address proves exactly one address.** It never proves a WHOIS
  record, an RDAP object, an ASN, or any enclosing prefix. Auto v1 is
  deterministic and reason-coded: a rule exists because a named, dated
  observation or an operator-declared range put it there, and the reason code says
  which.
- **A broad range enters only when a source that owns it publishes it.** An
  official operator feed may declare a prefix, because the operator is the
  authority on their own allocation. Nothing infers a prefix from an address.
- **Observations expire.** Validity comes from the source, and an expired
  observation leaves the artifact. That is what keeps a formerly correct address
  from routing a stranger's traffic a year later, and it is why the grace period
  is a reason code (`source_degraded`) rather than an extension of validity.
- **Observation validity and policy are separate concerns.** A sighting is valid
  or expired as a fact about time; whether it is accepted, rejected, or
  quarantined is a decision. Storing the decision as global sighting state would
  make one profile's policy change every other profile's data, so it is never
  stored that way.
- **Discovery widens names, not networks.** Deriving a service from a URL uses
  the Public Suffix List for the site boundary and one managed browser page load
  behind an in-process proxy. Same-site dependencies become a draft; third-party
  services are recorded as observed relations and never activated by a broad
  rule.
- **A format that can carry names should carry names.** Where the device resolves
  a name itself — a dnsmasq nftset, a RouterOS address-list entry, an AmneziaVPN
  site — the artifact carries the name and no address, so the routing decision
  follows the service instead of freezing a snapshot of it. Where a format cannot
  express a name, the observed addresses are carried and expire on schedule.

## Consequences

Coverage is honest rather than generous. A component whose names cannot be
expressed by the selected target and whose addresses have expired is reported as
partial coverage or refused; it is never approximated by a wider rule. Operators
see `partial_coverage` more often than a product that guesses would show it, and
that is the intended trade.

Every rule in every artifact can be traced to a named source and a date, which is
what makes the diagnostic view in the local UI meaningful and what makes a wrong
rule findable rather than mysterious.

The product cannot offer "route everything this company owns" as a feature. That
is not a gap to be closed later: it is the decision.

WHOIS, RDAP, and ASN lookups are absent from the dependency set entirely, so no
future change can quietly re-enable expansion by reusing an existing client. A
proposal to add one is a proposal to replace this ADR, not to extend it.
