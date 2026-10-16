---
status: adopted
---

# ADR 0057: an output may keep addresses beside the domains that cover them

Refines the domain-first rule of
[ADR 0010](0010-domain-first-and-no-network-expansion.md) with an operator
choice. Domain-first stays the default.

## Context

ADR 0010 says a format that can carry names should carry names. The planner
applies it by leaving out the addresses and third-party networks of a component
that a name already covers, with the reason
`not_required_for_domain_capable_target`. Two copies of the same coverage would
spend the device's rule budget twice, and an address is a snapshot while a name
follows the service. A network the operator publishes about itself is not
affected: an official declaration is kept, because it may reach endpoints no
name in the list resolves to.

A name rule works only when the device sees the lookup. A client that connects
by a cached address, a hard-coded one, or through a resolver that bypasses the
router's DNS reaches the service without a lookup the router can match, so the
domain rule never fires. Until now the operator had no way to get those
addresses back: the planner left them out, and no setting of the output asked
for them.

Four catalog formats take both kinds of rule: Keenetic DNS, MikroTik, sing-box
and AmneziaVPN. OpenWrt dnsmasq takes names only and Keenetic BAT takes
addresses only, so neither drops one kind for the other.

## Decision

An output has a boolean setting `keep_addresses`, off by default. When it is on,
the planner keeps the addresses and third-party networks that a name already
covers, beside the names.

- **The setting belongs to an output**, one format of one profile. It is offered
  only for a format that takes both names and addresses
  (`TargetConstraints.TakesNamesAndAddresses`). Setting it on another format is
  refused: `invalid_keep_addresses` from
  `PUT /v1/outputs/{id}/keep-addresses` and `invalid_shape` at
  `/outputs/<n>/keep_addresses` when a configuration transfer carries it.

- **Every value check still applies.** A kept address passes the same
  admissibility tests as any other: special-use addresses are refused as
  `special_use`, a prefix wider than ADR 0019 allows as `prefix_too_wide`, and a
  community-declared prefix is held to `/16` (IPv4) and `/32` (IPv6) by
  [ADR 0056](0056-community-prefixes-have-a-narrower-width.md). ADR 0056 already
  moved that judgment ahead of the domain-first test for this reason. Keeping
  addresses lets in only what an address would have been allowed anyway.

- **Nothing is widened.** One observed address still proves exactly that
  address. The setting changes whether a valid address is redundant, not what
  an address may claim.

- **A change takes effect on the next build.** Saving the setting invalidates
  that output's pending, unpublished build, as a prefix change does. A file
  already published is unchanged until the output is built again.

- **Storage and transfer.** The column `outputs.keep_addresses` (schema version
  21) defaults to 0, so every existing output keeps the policy it was built
  under. The transfer format stays `config-transfer-v1.4`; an output gains an
  optional `keep_addresses` field, omitted while false, so a file written
  without it imports as before.

## Alternatives considered

- **Per list.** A list is shared by every profile and every format that
  includes it, so a list-level switch would change output files the operator
  did not mean to touch. The need also depends on the device and its clients,
  not on the list. It would still have to be checked per format, because only
  some formats take both kinds of rule.

- **Per profile or global.** A profile publishes to several formats, and the
  rule budget (`rule_limit`) is counted per format. A global switch would
  apply to formats where it means nothing, and the build that goes over its
  limit would fail with no way to say which format asked for it. A per-output
  setting puts both the choice and its failure on the format. ADR 0049 already
  places the FQDN group prefix on the output for the same reason.

- **Always keep.** Every covered address would be rendered beside its name,
  spending the device's budget twice and refusing a build where one rendering
  would fit. Domain-first would stop being the default.

## Consequences

- A format with the setting on can reach `rule_limit` sooner. The build then
  fails for that format as any over-limit build does, and the previous valid
  file stays in place.

- With the setting on, a list that was covered by names alone now also carries
  addresses that expire on schedule, as ADR 0010 describes for formats without
  names.

- The weight forecast currently plans with default options, so with the
  setting on it can understate the built file. The build and its `rule_limit`
  check use the output's setting. Aligning the forecast is separate work. A
  command-line build of lists names no output and keeps the default.

- Database schema version 21 is one-way. An older Routevane, such as 0.4.1 at
  schema 20, refuses a database the new version has opened. The existing rule
  stands: use an older version with the backup taken before the update.

- The reason code `not_required_for_domain_capable_target` keeps its name but
  now marks only an address the format could carry, left out while the setting
  is off; its interface text names the switch that keeps it. An address the
  format cannot carry at all, such as any address for OpenWrt, is reported as
  `unsupported_by_target`, so the reason never points to a setting the format
  does not offer.
