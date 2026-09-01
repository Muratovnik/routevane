---
status: adopted
---

# ADR 0016: categories group lists; a list references, and the output deduplicates

> Current scope: UI vocabulary and operator-owned categories were revised by ADR 0028–0029.
> The original catalog-only scope below is historical; see [current UI contract](../UI.md).

## Context

ADR 0013 made the list the product unit and shipped presets as ready-made
service bundles: instantiating a preset copies its composition into a new list,
and an optional hybrid follow mode resolves `preset ∪ added − excluded` at read
time. A preset was therefore a template, and a list was a flat set of services.

Sizing the first real preset set on 2026-08-22 broke that shape. Several
services belong in more than one grouping — Discord is both communication and
gaming, Cloudflare is infrastructure behind almost everything. Under copying,
a user who takes two presets gets two lists that each name Discord, and both
render Discord's rules into their own artifact. The store holds no duplicate,
because a service is referenced rather than copied, but the **device** pays
twice: two artifacts carry the same prefixes, against a rule budget that is
shared and small. The owner rejected the shape: a grouping is a category over
lists, not a list of its own, and membership must be by reference.

The follow mode already conceded the same point without naming it. A list that
receives later preset additions is holding a reference, not a copy; only the
depth was undecided.

## Decision

- A **list** is the unit of storage and of output. It holds its own entries,
  one-level **references** to catalog categories and shipped lists, and
  **exclusions** that remove named members of what it references. A shipped
  list such as YouTube and a list the user assembles are the same kind of
  object.
- A **category** is a named grouping of lists, many-to-many. It stores no
  entries of its own and owns no data. A **preset is a category** that
  Routevane ships; instantiating one is no longer a copy.
- **A list may not reference another user list.** References point only at
  catalog-shipped categories and lists, which cannot themselves reference user
  content. Cycles are therefore impossible by construction and no cycle
  detection exists to be got wrong. Arbitrary nesting was refused: it buys
  nothing the one-level form cannot express and costs cycle control plus a tree
  walk in every diagnostic.
- **Deduplication moves from the list to the output.** At build time an output
  resolves every reference into one flat set, drops exclusions, deduplicates,
  and only then plans and renders. A service reachable through two categories
  is rendered once, and the rule budget is charged once.
- **The list is the selection, so an output still binds exactly one list.**
  "Everything in Видео and Музыка" is a list that references both categories,
  not an output that names two lists. Letting an output carry its own selection
  would put the same expression in two places and give the operator nothing it
  cannot already say. What an output produces is one logical artifact, split
  only when the target demands it (ADR 0017).
- Resolution is a **read-time** operation, as follow mode already was. A
  category that gains a list later reaches every list referencing it without an
  edit, and the list's history records that the category — not the user — added
  it.

## Consequences

The schema gains categories, category membership, list references, list
entries and exclusions. Existing lists migrate as lists holding their current
services as entries with no references, so nothing a user built changes meaning
and no subscription is disturbed (ADR 0004).

ADR 0013's preset clauses — presets as copied bundles, and hybrid follow as a
special mode — are superseded by this ADR. Everything else in ADR 0013 stands:
the list is still the unit, the output still owns the subscription and the
artifact history, and a save still rebuilds.

The consequence ADR 0013 recorded about a followed list having no single stored
composition now applies to every list that references anything. The same
honesty rule holds: if a referenced category leaves the catalog, the last
resolved composition is retained and the list reports the reference as gone
rather than silently shrinking.

Deduplication at the output makes the rule count depend on the whole selection
rather than on any one list, so a budget overflow can no longer be attributed
to a single list. The diagnostic must name the selection and the contributors,
not blame the last list added.

A category is catalog-supplied in this slice. Whether a user may define their
own category is deliberately left open; nothing here forbids it later, because
a category owns no data and adding a user-defined one adds a row, not a model.
