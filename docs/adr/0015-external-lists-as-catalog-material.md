---
status: adopted
---

# ADR 0015: public lists are material for our catalog, not the catalog itself

## Context

Presets need a catalog worth grouping, and the shipped catalog holds two
services. The owner named seven public routing/blocklist projects as material.
A survey of all seven established what they actually are:

- **itdoginfo/allow-domains** — sixteen per-service files plus categories,
  domains and ASN-derived subnets, every file inside the 1024-rule budget,
  a weekly cron and dated releases. It carries **no license file at all**.
- **v2fly/domain-list-community** — 1536 per-service files, domains only, MIT,
  tagged releases. Its format is a DSL with `include:`, `full:`, `keyword:`
  and `regexp:`, not a plain list.
- **iplist.opencck.org** — a live service that resolves configured domains and
  aggregates CIDRs on demand. Per-service via a query parameter, MIT engine,
  the widest format coverage. Its answers are recomputed, so no revision can
  be pinned, and unaggregated address output is two orders of magnitude over
  the rule budget.
- **1andrevich/Re-filter-lists** — the best-documented methodology of the
  seven, MIT, but 81k domains and 27k prefixes in undifferentiated blobs with
  almost no per-service split.
- **RockBlack-VPN/ip-address**, the **iamwildtuna gist** and
  **MetaCubeX/meta-rules-dat** were not adopted: Windows `route ADD` scripts
  and inconsistent filenames, an unlicensed unstructured single file with no
  per-item URL, and force-pushed branches with a single overwritten `latest`
  release respectively.

Two facts about this product decided the shape of the answer. The HTTP feed
source parses addresses and prefixes only and silently drops domains, which
excludes the domain-first sources outright. And every feed observation is
recorded as `official`, which would file a stranger's curation under
"the vendor's own published ranges" in the planner's vocabulary.

## Decision

**A public list is material, never the product's own taxonomy.** Routevane
defines its own services with its own identifiers, titles and categories, and
each service names whichever upstream sources serve it best. No source is
mirrored one to one, and no upstream's grouping becomes ours.

- The HTTP feed learns to carry **domains** alongside addresses. Without it the
  product cannot consume the domain-first material it prefers, while claiming
  domain-first as its policy.
- A feed **declares its source class** in the catalog, and a third-party list
  declares `community`. `official` stays for ranges a service publishes about
  itself. Diagnostics then say where a rule came from without lying.
- Feeds are **fetched live over HTTPS** at refresh from a pinned URL. Nothing
  is vendored into this repository: the product ships the address of a list,
  never a copy of it. The existing grace window covers an unreachable source.
- **Over the device budget, aggregate losslessly first, then refuse.** A build
  that still exceeds the target's rule limit fails and names the limit and the
  overflow. It never truncates: a silently shortened route table is worse than
  no file.
- **Several sources may serve one service.** Duplicate values collapse, and the
  planner decides which shape a target actually needs. A target that routes by
  name drops the addresses those names resolve to and a third party's
  aggregation of them, because both are a rendering of the same names and
  carrying them spends the device's budget twice for the same coverage. A range
  the operator publishes about itself is kept: it is not derived from these
  names and may serve endpoints no name in the list reaches.
- **A curated prefix list is a declaration, not an inference.** The planner had
  admitted a published prefix only from the `official` class, which would have
  made declaring `community` honestly the reason none of this material could be
  routed. A prefix published as a prefix is admitted from either class; an
  address, an ASN or an RDAP record still proves nothing about a network, and
  explicit shared-network evidence still quarantines the prefix. Which class
  declared it travels with the rule into the diagnostics.
- **Domains are taken as suffixes, and iplist supplies addresses only.** A
  target that expands subdomains itself needs the suffix form; an enumerated
  host list is the wrong material for it and inflates the result by orders of
  magnitude. Measured on 2026-08-22, YouTube is 18 suffix entries at itdoginfo
  and 178 at v2fly, against 786 collapsed CIDRs and 15 312 enumerated hostnames
  from iplist. Suffixes therefore come from itdoginfo and v2fly; iplist
  contributes `cidr4`/`cidr6` and nothing else.
- **Only per-service material is imported.** Upstream category files are not
  adopted, because a category the operator cannot open and edit is a bundle
  pretending to be a service.
- **v2fly's DSL is parsed strictly and partially**: bare domains and `full:`
  are taken, `keyword:` and `regexp:` are refused, `include:` is not expanded.
  The count of skipped lines is a diagnostic, not a silence.
- **1andrevich is used by intersection only.** Its registry blob contributes
  where it overlaps the domains of our own services; the remainder is not
  imported, because 81k unclassified domains are not a routing decision anyone
  made.

## Consequences

The product gains a network dependency on projects it does not control. That is
the point of a live feed, and it is bounded by the existing per-feed byte and
entry limits, the SSRF policy, and the grace window.

**itdoginfo carries no license.** The owner decided on 2026-08-22 to use it
anyway. The exposure is narrowed by the live-feed rule above: this repository
distributes a URL, and the operator's own machine fetches the data. That is a
mitigation, not a license, and the risk is recorded here rather than assumed
away. If the project adds a license that forbids this use, the catalog entry is
removed.

iplist.opencck.org cannot be pinned, so two refreshes a day apart may legally
disagree. The observation lifecycle already treats that as ordinary: values
expire, and a plan states what it was built from.

The catalog stops being hand-written data and becomes curation work with an
upstream. A service whose upstream disappears keeps its stored observations
until they expire and reports its source as failed, exactly as any other source
does.
