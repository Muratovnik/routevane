---
status: adopted
---

# ADR 0017: a second Keenetic target renders FQDN object groups

## Context

ADR 0003 added one Keenetic target: a BAT file of IPv4 static routes, bounded
at the documented 1024 lines. That bound was then treated as a property of the
device. It is not. It is the documented limit of one import mechanism —
verbatim, *"Number of lines in a bat file containing static routes: up to
`1024`"*.

KeeneticOS 5.0 added DNS-Based Routes, a different mechanism entirely. A named
`object-group fqdn` holds domain names and IPv4/IPv6 addresses; subdomains of a
listed name are included automatically and `*` is rejected. `dns-proxy route
object-group {group} [{interface} | {gateway} [interface]] [auto] [reject]`
points a group at an interface or gateway. KeeneticOS 5.2 adds the same under a
connection policy and Punycode names.

Measuring the material settled the case. YouTube is 18 suffix entries at
itdoginfo and 178 at v2fly, against 786 collapsed CIDRs and 15 312 enumerated
hostnames from iplist. Discord, Telegram, Meta, TikTok, Netflix and OpenAI are
all between 11 and 36 suffix entries. The domain form of a service is two to
three orders of magnitude smaller than its address form, and the rule budget
stops being the constraint that shapes the product.

Published numeric limits are thin. The device limit table gives no entry bound
for an FQDN group. The only published count is a changelog line, *"IPv4:
increased the object groups limit to 128"*, written in 5.0 Alpha 1 — eight alpha
releases before FQDN object groups existed, so whether it governs them is not
established. Field practice sets 300 entries per group as a configurable
default. The owner decided on 2026-08-22 not to design around undocumented
limits, since they are outside our control.

## Decision

- Add target `keenetic-dns`, profile `keenetic-fqdn-group-v1`, renderer
  `keenetic-fqdn-group` for KeeneticOS 5.0 or later. Target `keenetic` and its
  BAT renderer stay for earlier firmware and for address-only lists.
- The renderer emits **domain suffixes**, never enumerated hostnames, because
  the device expands subdomains itself. A source that enumerates hosts is the
  wrong material for this target (ADR 0015 is amended accordingly).
- **Every published limit lives in the target profile as data**, not in code:
  `max_entries_per_list` and `max_lists` join the existing `max_rules`. An
  undocumented bound is recorded as our chosen value with its provenance, so a
  firmware change is a YAML edit and never a release.
- **A list that exceeds `max_entries_per_list` is split into sub-groups**
  automatically, named `<group>`, `<group>-2`, `<group>-3`. The operator asked
  for one list and gets one list; the split is the adapter's business. Each
  sub-group receives its own `dns-proxy route` line.
- **Group names are namespaced with a Routevane prefix.** The 128-group budget
  is device-wide and shared with groups a person created by hand or with
  another tool. We never write a name we did not create.
- Splitting for this target produces **one artifact**. Groups are sections of a
  single command file, so the publication model, the subscription URL and the
  download are untouched.
- **`max_lists` is charged across the whole device, not per output.** Two
  outputs aimed at one device compete for the same 128 groups, and the overflow
  diagnostic names the device.
- Over `max_lists` the build **refuses and names the budget**, exactly as an
  over-budget BAT build does. Splitting creates room inside a file format; it
  never creates device capacity.

## Consequences

Reapplying a changed list leaves the previous sub-groups on the device, and
after a few refreshes they exhaust the group budget. Reconciling device state —
removing the groups we previously wrote before writing the new ones — is a new
capability: Routevane currently produces files and owns nothing on the router.
It is required before automatic delivery to this target.

The sentence that stood here also claimed the generated file carries the removal
commands for the groups it replaces. It never did, and it cannot: the file is a
function of the plan alone, so it does not know what the device already holds,
and deleting a group would take the operator's own `dns-proxy route` with it.
Reconciliation is decided in ADR 0019 and lives in the deployer, which can read
the device; the manual path adds and updates but does not remove.

Two device-side requirements are the operator's, and Routevane states them
rather than enforcing them: on 5.0 and 5.1 DNS-based routing works only with
the Default connection policy, and a client's DNS must be the router or the
router never sees the answer that triggers the route. The owner accepted both
on 2026-08-22 as the user's concern.

Splitting the BAT target is deliberately **not** decided here. There 1024 is a
per-file limit, so splitting means several files, which turns an artifact into
a set and reaches the publication model, the subscription and the download.
Its import is additive and non-transactional (ADR 0003), so a run that stops at
the third file leaves partial coverage with no signal. Given how small the
domain form is, that work waits for evidence that anyone needs it.

Hardware acceptance is unverified for this target as it is for BAT. The file
contract comes from maintained documentation and the published command
reference, and ADR 0003's rule applies unchanged: if documentation or hardware
disproves it, disable the target and supersede this ADR.

## Official sources

- [DNS-based routes](https://support.keenetic.com/explorer/kn-1613/en/51150-dns-based-routes.html)
- [Functional limitations of devices](https://support.keenetic.com/hero/kn-1012/en/49454-functional-limitations-of-devices.html)
- [KeeneticOS 5.0 changelog](https://forum.keenetic.com/topic/21084-changelog-50/)
- [KeeneticOS 5.2 development release](https://support.keenetic.com/peak/kn-2710/en/9188-latest-development-release.html)
