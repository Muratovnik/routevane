---
status: adopted
---

# ADR 0019: an observed value becomes a route only if it is a destination

## Context

ADR 0010 settled where a route may come from: a name is first, and one observed
address never proves a suffix, an ASN, an RDAP owner, or a broad network. That
decision is about *provenance* — who is entitled to claim a network. It says
nothing about the *value* itself.

`security-boundaries.md` already treats every feed, DNS answer, HAR file and
device reply as hostile input, and `netpolicy` owns the rule that decides which
addresses Routevane may open a connection to. That policy was applied at every
outbound boundary and nowhere else. An address that arrived as an observation and
became a routing rule was checked for syntax and for provenance, never for what
it is.

The gap was not theoretical. The browser acceptance suite pinned, as an expected
artifact, the line:

```
route ADD 127.0.0.1 MASK 255.255.255.255 0.0.0.0
```

Its fixture pointed a DNS source at `localhost` to stay hermetic, the planner
accepted the answer, the renderer wrote it, and the test asserted it. Loopback
was reaching a router's routing table, and the suite was guarding that it kept
doing so.

The same path admits worse. A community feed is entitled to declare its own
networks, so `declaredNetwork` accepts its prefixes; nothing bounded their width
or their class. A feed that shipped `10.0.0.0/8` would take the operator's own
LAN into the tunnel. `0.0.0.0/1` would take half the internet. Both would be
published as reason-coded, provenance-carrying, perfectly auditable rules.

## Decision

- **A routing rule's value is judged, not only its provenance.** The planner
  refuses a candidate whose own value cannot be a destination, before any target
  or renderer sees it. This is a policy decision, so it belongs to the planner
  and it is reason-coded like every other one: `special_use_destination` for a
  class that is never a destination, `prefix_too_wide` for a prefix that
  swallows an address space.

- **The judgement is `netpolicy`'s, in the package that already owns it.** The
  question "may this be a destination" is not the same as "may we connect to
  this", so it is a second named function rather than a reuse of the first —
  but it lives beside it and shares its classification. That package exists
  because a destination policy two callers each reimplement is a policy that
  will diverge, and a second copy in the planner would have been exactly that.

- **The two questions differ in one range, deliberately.** Benchmarking space
  (RFC 2544) may appear in a plan and may not be connected to. Steering it
  reaches nobody and costs nothing, while refusing it would remove the only
  address class a test can use that is neither local nor a real host — the same
  reason `PublicUnicast` keeps documentation ranges allowed. Every range that
  makes an address local, unreachable, or a metadata service is refused by both.

- **Provenance does not buy an exemption.** A loopback address declared by an
  official catalog seed is still loopback. The check runs on seeds and on
  sightings alike, because the failure it prevents is a property of the value.

- **Width is bounded independently of who claims it.** An IPv4 prefix shorter
  than `/8`, or an IPv6 prefix shorter than `/16`, is refused. The bound is
  deliberately loose: it is not an optimizer and not a judgement about how much
  of the internet a service may legitimately own. It is the line past which a
  single rule stops being a route and becomes a default gateway.

- **A prefix is checked over its entire range.** Its first address being allowed
  is insufficient: `172.0.0.0/10`, for example, also covers the private
  `172.16.0.0/12`. Any overlap with a forbidden destination range rejects the
  whole candidate. Address and prefix checks use the same range classification;
  documentation and benchmarking destinations retain their existing allowances.

- **A name covers the addresses reported with it.** The exclusion that drops
  addresses a name already covers now consults coverage accepted during the same
  pass, not a snapshot taken before the observations were read. Names are read
  before addresses so a feed that answers with both in one response spends the
  device's budget once. This is a reading order for one pass and never reaches
  the plan's canonical order or its semantic hash.

## Consequences

- A catalog or a feed that relied on a special-use value now sees it excluded
  with a reason code instead of published. That is visible in diagnostics, which
  is where a refusal belongs.

- The browser fixture that pinned a loopback route was changed to seed a
  documentation-range destination while its DNS source still answers with
  loopback. The suite now proves the opposite of what it used to: the routable
  value reaches the device and the loopback observation does not.

- Coverage can become incomplete where it previously looked complete, and a
  build fails closed rather than publishing a rule set built from
  non-destinations. Failing closed is the same choice ADR 0004 made for a
  candidate that cannot be validated.

- A default route reaching a renderer is now refused one step earlier, by policy
  rather than by preflight. The guarantee is unchanged and stronger: no renderer
  and no output ever sees it.
