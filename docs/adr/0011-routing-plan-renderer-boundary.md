---
status: adopted
---

# ADR 0011: the routing plan / renderer boundary

## Context

This is the second decision the plan requires as a minimum architectural
document, and the one that has been tested most often: the product now carries
five built-in formats, two deployers, and an external plugin protocol, all added
without changing the planner.

The boundary answers one question: when an artifact is wrong, which component is
at fault? Without a hard line, the answer is always "some of both". A renderer
that knows about services starts deciding what to include; a planner that knows
about a file format starts producing rules shaped for one device. Both drifts end
in the same place — every new format is a change to policy, every policy change
risks every format, and no artifact can be explained without reading all of it.

## Decision

Policy decisions live in the planner and are expressed once, in a
`RoutingPlan`. A renderer projects that plan into one format and decides nothing.

- **The planner decides what to route.** Which components are covered, which
  observations are still valid, which rules a target's capabilities allow, what
  the reason code is, and whether coverage is complete, partial, or insufficient.
  It knows target *capabilities* — can this target express a suffix, an IPv6
  prefix, a dynamic set — and nothing about bytes.
- **A renderer owns one format and only format.** It projects already-decided
  rules. It may deduplicate values the format cannot express twice and it must
  order them deterministically, but it never adds, drops, reorders, or
  reinterprets a policy decision. A renderer that needs to know about services,
  persistence, or devices is a renderer being asked to do someone else's job.
- **Device limits live in the target profile, not in either.** Rule counts, byte
  bounds, and capability flags are catalog data validated against what the
  renderer can actually express; a profile claiming more is dropped rather than
  offered. A `frozenProfiles` entry pins the device-specific truth a catalog file
  must not widen.
- **A renderer validates its own output independently.** `Parse` decodes without
  reusing `Render`, then the projection is rendered again and byte equality is
  required. That single rule is what makes non-canonical, padded, reordered, and
  duplicated documents impossible to publish, and it is why an artifact can be
  trusted by a deployer that never parses a plan.
- **The plan and the artifact are stored separately.** A `PlanSnapshot` records
  what was decided; an `ArtifactBuild` records what one format made of it. A
  failed render therefore loses a format, never a decision, and the previous
  valid artifact stays published.
- **Deployment installs bytes the publication path already proved.** A deployer
  never renders and never plans. It may validate an artifact with the renderer's
  own parser — that is a check, not a projection.
- **Registries are separate maps.** The target catalog resolves a device label to
  a profile; the renderer registry resolves a format id to an implementation; the
  deployer registry is keyed by renderer id, because what can be installed is
  decided by the format rather than by the device label. Several targets may share
  one renderer.

## Consequences

Adding a format is a new package, a catalog file, a registry line, and its own
tests. The planner is untouched, which is why "formats added without changing the
planner" is a stated quality metric and why five formats now coexist with one
policy implementation.

The same plan reaching two targets produces visibly different artifacts — the
router dialect carries addresses the rule-set format does not need, the dnsmasq
fragment carries a name and no address at all. That difference is the boundary
working, not an inconsistency, and the end-to-end tests assert it directly.

An external plugin renderer is indistinguishable downstream: it receives the same
plan projection, its output passes the same publication path and size bound, and
it may not claim a capability it cannot express. The plugin protocol was
implementable at all because this boundary already existed
([`0009-plugin-protocol-over-stdio.md`](0009-plugin-protocol-over-stdio.md)).

What is given up is per-device policy. A target cannot ask for "the same plan but
with these three rules dropped": a different decision is a different plan, made by
the planner, with its own snapshot and its own reason codes.
