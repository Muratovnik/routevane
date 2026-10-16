---
status: adopted
---

# ADR 0056: a community-declared prefix has a narrower width bound

## Context

ADR 0015 admitted `community` as a declaring class: a curated third-party list
may declare networks, and `declaredNetwork` accepts its prefixes. ADR 0019
then bounded every prefix by width, independently of who claims it: an IPv4
prefix shorter than `/8` or an IPv6 prefix shorter than `/16` is refused. That
bound was chosen as the line past which a rule becomes a default gateway, not
as a judgement about how much address space a service may own.

The shipped lists read such third-party material. The DeepSeek list takes its
IPv4 networks from an aggregated per-site feed declared `community`. That feed
publishes whole regional and cloud blocks for one service, among them
`60.0.0.0/8`, `112.0.0.0/8`, `8.208.0.0/12` and `38.0.0.0/7`. All but the `/7`
passed both checks. A published static-route file carried
`route ADD 60.0.0.0 MASK 255.0.0.0` and sent a region's traffic into the tunnel
for one service.

A curator attributes networks to a service; it does not own them. Its
declaration is better evidence than one observed address and weaker than a
vendor's statement about its own ranges.

## Decision

- **A prefix declared by a `community` source is refused when it is wider than
  `/16` for IPv4 or `/32` for IPv6.** The refusal carries the existing reason
  `prefix_too_wide`: for the operator it is the same fault as the general
  bound.

- **The general bound of ADR 0019 is unchanged for every other class.** An
  `official` declaration keeps `/8` and `/16`. The operator's own entry keeps
  it too, because it is the operator's own choice.

- **The bound follows the declaring class, not the path.** A catalog seed
  declared `community` is held to it, like a feed sighting.

- **A value is judged before its necessity.** The planner refuses a candidate
  whose value is inadmissible (special-use or too wide) before it considers
  whether a name already covers that candidate's component. Previously a
  domain-capable target reported such a value as `not_required_for_domain_capable_target`.
  That hid the fault, and it would have let the value through as soon as an
  operator chose to keep addresses beside names.

## Consequences

- On the shipped lists as observed on 2026-10-16, about a fifth of the
  community prefixes (1475 of 7551, all IPv4 from `/7` to `/15`) are refused;
  every list kept some of its networks. That share is the intended effect.

- A list whose only network evidence is wide community blocks loses those
  networks. On a target without name rules this can leave a required component
  uncovered, and the build then fails closed as ADR 0019 already describes. The
  diagnostics name the refused blocks.

- Lossless collapse still merges admitted community prefixes. A collapsed
  prefix wider than `/16` stands for exactly the addresses the narrower
  declarations covered, so it is not a new claim and is not re-judged.

- `/16` and `/32` are a policy line, not a measurement of any feed. Raising or
  lowering them changes which community material is usable, and is a decision
  for a new ADR.
